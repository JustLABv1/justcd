package render

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/justlab/justcd/services/backend/internal/core"
	"sigs.k8s.io/yaml"
)

// chartFetchTimeout bounds one registry login plus chart download.
const chartFetchTimeout = 2 * time.Minute

// RegistryAuth is a decrypted helm-registry credential. It is never part of a
// render fingerprint or a persisted plan.
type RegistryAuth struct {
	// Endpoint is an oci:// or https:// location, normalized by
	// NormalizeRegistryEndpoint. It applies to that path and everything below.
	Endpoint string
	Username string
	Password string
	// CAData is an optional PEM bundle trusted in addition to the system roots.
	CAData string
}

type registryLocation struct {
	scheme string
	host   string // lowercase host, with the port when one is given
	path   string // cleaned path without a trailing slash; "" for the root
}

func parseRegistryLocation(raw string) (registryLocation, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "oci" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return registryLocation{}, errors.New("registry location must be an oci:// or https:// URL without credentials, query, or fragment")
	}
	cleaned := strings.TrimSuffix(path.Clean("/"+parsed.Path), "/")
	if cleaned != strings.TrimSuffix(parsed.Path, "/") && !(parsed.Path == "" && cleaned == "") {
		return registryLocation{}, errors.New("registry location path must not contain empty, '.' or '..' segments")
	}
	host := strings.ToLower(parsed.Hostname())
	if port := parsed.Port(); port != "" {
		host = net.JoinHostPort(host, port)
	}
	return registryLocation{scheme: parsed.Scheme, host: host, path: cleaned}, nil
}

// NormalizeRegistryEndpoint validates a helm-registry credential endpoint and
// returns its canonical form.
func NormalizeRegistryEndpoint(raw string) (string, error) {
	if len(raw) > 512 {
		return "", errors.New("registry endpoint must be at most 512 characters")
	}
	location, err := parseRegistryLocation(raw)
	if err != nil {
		return "", err
	}
	return location.scheme + "://" + location.host + location.path, nil
}

// ValidateRegistryCA checks that an optional CA bundle holds PEM certificates.
func ValidateRegistryCA(data string) error {
	if strings.TrimSpace(data) == "" {
		return nil
	}
	if len(data) > 64<<10 {
		return errors.New("registry CA bundle exceeds 64 KiB")
	}
	rest, found := []byte(data), false
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			return errors.New("registry CA bundle may contain only PEM certificates")
		}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			return errors.New("registry CA bundle contains an invalid certificate")
		}
		found = true
	}
	if !found || strings.TrimSpace(string(rest)) != "" {
		return errors.New("registry CA bundle must contain PEM certificates")
	}
	return nil
}

// matchRegistryAuth returns the most specific credential whose endpoint
// contains reference: same scheme and host, and a path prefix on a segment
// boundary.
func matchRegistryAuth(auths []RegistryAuth, reference string) *RegistryAuth {
	target, err := parseRegistryLocation(reference)
	if err != nil {
		return nil
	}
	var best *RegistryAuth
	bestLength := -1
	for i := range auths {
		endpoint, err := parseRegistryLocation(auths[i].Endpoint)
		if err != nil || endpoint.scheme != target.scheme || endpoint.host != target.host {
			continue
		}
		if endpoint.path != "" && target.path != endpoint.path && !strings.HasPrefix(target.path, endpoint.path+"/") {
			continue
		}
		if len(endpoint.path) > bestLength {
			best, bestLength = &auths[i], len(endpoint.path)
		}
	}
	return best
}

func validateHelmRepository(parent context.Context, value string) error {
	return validateChartRepository(parent, value, value, nil)
}

// validateChartRepository applies the network policy for a chart location.
// Without a matching credential only public HTTPS/OCI hosts on port 443 are
// allowed. A matching credential represents an endpoint trusted by an instance
// administrator, so private hosts and other ports are then allowed.
func validateChartRepository(parent context.Context, repository, reference string, auths []RegistryAuth) error {
	parsed, err := url.Parse(repository)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "oci") || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("Helm chart repository must be an https:// or oci:// URL without credentials")
	}
	if matchRegistryAuth(auths, reference) != nil {
		return nil
	}
	if parsed.Port() != "" && parsed.Port() != "443" {
		return errors.New("Helm chart repository must use port 443 unless a registry credential is configured for it")
	}
	host := strings.ToLower(parsed.Hostname())
	if net.ParseIP(host) != nil || host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return errors.New("Helm chart repository must use a public DNS hostname unless a registry credential is configured for it")
	}
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	addresses, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return errors.New("Helm chart repository hostname could not be resolved")
	}
	for _, address := range addresses {
		if !address.IsGlobalUnicast() || address.IsPrivate() {
			return errors.New("Helm chart repository must resolve only to public addresses unless a registry credential is configured for it")
		}
	}
	return nil
}

// helmRegistrySetup is the result of configuring an isolated Helm home.
type helmRegistrySetup struct {
	env []string
	// repoAliases maps an authenticated HTTPS repository URL to the name it
	// was added under, for "<alias>/<chart>" references.
	repoAliases map[string]string
}

// prepareHelmRegistries writes credentials for references into the isolated
// Helm home: a registry config for OCI, `helm repo add` for HTTPS (password
// on stdin), and extra CA roots via SSL_CERT_DIR. Secrets never appear in
// command arguments.
func prepareHelmRegistries(ctx context.Context, home string, env []string, dir string, auths []RegistryAuth, references []string) (helmRegistrySetup, error) {
	setup := helmRegistrySetup{env: env, repoAliases: map[string]string{}}
	ociAuths := map[string]*RegistryAuth{}
	httpsRepos := map[string]*RegistryAuth{}
	caAuths := map[*RegistryAuth]bool{}
	for _, reference := range references {
		auth := matchRegistryAuth(auths, reference)
		if auth == nil {
			continue
		}
		location, err := parseRegistryLocation(reference)
		if err != nil {
			return setup, err
		}
		if strings.TrimSpace(auth.CAData) != "" {
			caAuths[auth] = true
		}
		if location.scheme == "oci" {
			if existing, ok := ociAuths[location.host]; ok && existing != auth {
				return setup, fmt.Errorf("one render cannot use two different registry credentials for %s", location.host)
			}
			ociAuths[location.host] = auth
			continue
		}
		httpsRepos[strings.TrimSuffix(strings.TrimSpace(reference), "/")] = auth
	}
	// Helm keys OCI credentials by host. Do not add a credential when the
	// same command also pulls a chart outside that credential's endpoint.
	for _, reference := range references {
		location, err := parseRegistryLocation(reference)
		if err != nil {
			return setup, err
		}
		if auth := ociAuths[location.host]; location.scheme == "oci" && auth != nil && matchRegistryAuth(auths, reference) != auth {
			return setup, fmt.Errorf("one render cannot mix authenticated and unauthenticated charts on OCI host %s", location.host)
		}
	}
	if len(caAuths) > 0 {
		certDir := filepath.Join(home, "registry-ca")
		if err := os.MkdirAll(certDir, 0700); err != nil {
			return setup, err
		}
		index := 0
		for auth := range caAuths {
			if err := os.WriteFile(filepath.Join(certDir, fmt.Sprintf("registry-%d.pem", index)), []byte(auth.CAData), 0600); err != nil {
				return setup, err
			}
			index++
		}
		setup.env = withExtraCertDir(setup.env, certDir)
	}
	if len(ociAuths) > 0 {
		config := struct {
			Auths map[string]map[string]string `json:"auths"`
		}{Auths: map[string]map[string]string{}}
		for host, auth := range ociAuths {
			config.Auths[host] = map[string]string{"auth": base64.StdEncoding.EncodeToString([]byte(auth.Username + ":" + auth.Password))}
		}
		data, err := json.Marshal(config)
		if err != nil {
			return setup, err
		}
		configPath := filepath.Join(home, ".config", "helm", "registry", "config.json")
		if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
			return setup, err
		}
		if err := os.WriteFile(configPath, data, 0600); err != nil {
			return setup, err
		}
		setup.env = append(setup.env, "HELM_REGISTRY_CONFIG="+configPath)
	}
	repositories := make([]string, 0, len(httpsRepos))
	for repository := range httpsRepos {
		repositories = append(repositories, repository)
	}
	sort.Strings(repositories)
	for index, repository := range repositories {
		auth := httpsRepos[repository]
		alias := fmt.Sprintf("justcd-registry-%d", index)
		args := []string{"repo", "add", alias, repository, "--username", auth.Username, "--password-stdin"}
		if _, err := execRenderer(ctx, "helm", args, dir, setup.env, auth.Password); err != nil {
			return setup, err
		}
		setup.repoAliases[repository] = alias
	}
	return setup, nil
}

// withExtraCertDir adds dir to Go's certificate directories. Go reads the
// default CA bundle files as well, so the system roots stay trusted.
func withExtraCertDir(env []string, dir string) []string {
	existing := "/etc/ssl/certs"
	out := make([]string, 0, len(env)+1)
	for _, pair := range env {
		key, value, _ := strings.Cut(pair, "=")
		if strings.EqualFold(key, "SSL_CERT_DIR") {
			if value != "" {
				existing = value
			}
			continue
		}
		out = append(out, pair)
	}
	return append(out, "SSL_CERT_DIR="+dir+string(os.PathListSeparator)+existing)
}

// runHelmWithRegistries runs one Helm command in a fresh isolated home after
// configuring credentials for references. args receives the setup so HTTPS
// charts can be addressed through their authenticated repository alias.
func runHelmWithRegistries(parent context.Context, dir string, auths []RegistryAuth, references []string, args func(helmRegistrySetup) []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, chartFetchTimeout)
	defer cancel()
	home, err := os.MkdirTemp("", "justcd-helm-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(home)
	setup, err := prepareHelmRegistries(ctx, home, cleanEnvironment(os.Environ(), home), dir, auths, references)
	if err != nil {
		return nil, err
	}
	return execRenderer(ctx, "helm", args(setup), dir, setup.env, "")
}

// pullHelmChart downloads and unpacks chart into destination and returns the
// chart directory. destination must not already contain the chart.
func pullHelmChart(ctx context.Context, chart core.HelmChartSource, auths []RegistryAuth, dir, destination string) (string, error) {
	chart, err := chart.Normalize()
	if err != nil {
		return "", err
	}
	if err := validateChartRepository(ctx, chart.Repository, chart.Reference(), auths); err != nil {
		return "", err
	}
	if err := os.MkdirAll(destination, 0700); err != nil {
		return "", err
	}
	_, err = runHelmWithRegistries(ctx, dir, auths, []string{chart.Reference()}, func(setup helmRegistrySetup) []string {
		args := []string{"pull"}
		switch alias, ok := setup.repoAliases[chart.Repository]; {
		case chart.IsOCI():
			args = append(args, chart.Reference())
		case ok:
			args = append(args, alias+"/"+chart.Chart)
		default:
			args = append(args, chart.Chart, "--repo", chart.Repository)
		}
		return append(args, "--version", chart.Version, "--untar", "--untardir", destination)
	})
	if err != nil {
		return "", err
	}
	chartDir := filepath.Join(destination, chart.Chart)
	info, err := os.Lstat(chartDir)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("the downloaded archive does not contain chart %q", chart.Chart)
	}
	return chartDir, nil
}

type chartDependency struct {
	Name       string `json:"name"`
	Repository string `json:"repository"`
}

// ensureHelmDependencies runs `helm dependency build` when Chart.yaml lists
// dependencies that are not vendored in charts/. Vendored charts render
// exactly as before, without network access.
func ensureHelmDependencies(ctx context.Context, chartPath, repositoryRoot string, auths []RegistryAuth) error {
	content, err := os.ReadFile(filepath.Join(chartPath, "Chart.yaml"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(content) > maxManifestFileBytes {
		return errors.New("Chart.yaml exceeds 2 MiB")
	}
	var metadata struct {
		Dependencies []chartDependency `json:"dependencies"`
	}
	if err := yaml.Unmarshal(content, &metadata); err != nil {
		return errors.New("Chart.yaml is invalid")
	}
	if len(metadata.Dependencies) > 64 {
		return errors.New("Chart.yaml may list at most 64 dependencies")
	}
	missing := false
	for _, dependency := range metadata.Dependencies {
		if strings.TrimSpace(dependency.Repository) != "" && !dependencyVendored(chartPath, dependency.Name) {
			missing = true
		}
	}
	if !missing {
		// Fully vendored charts never contact their repositories.
		return nil
	}
	references := []string{}
	for _, dependency := range metadata.Dependencies {
		if strings.TrimSpace(dependency.Repository) == "" {
			continue
		}
		reference, err := dependencyReference(ctx, chartPath, repositoryRoot, dependency, auths)
		if err != nil {
			return fmt.Errorf("Helm dependency %q: %w", dependency.Name, err)
		}
		if reference != "" {
			references = append(references, reference)
		}
	}
	_, err = runHelmWithRegistries(ctx, repositoryRoot, auths, references, func(helmRegistrySetup) []string {
		return []string{"dependency", "build", chartPath}
	})
	return err
}

// dependencyReference validates a Chart.yaml dependency source and returns
// its credential-matching reference ("" for file:// dependencies).
func dependencyReference(ctx context.Context, chartPath, repositoryRoot string, dependency chartDependency, auths []RegistryAuth) (string, error) {
	repository := strings.TrimSpace(dependency.Repository)
	if dependency.Name == "" || strings.ContainsAny(dependency.Name, `/\`) || dependency.Name == "." || dependency.Name == ".." {
		return "", errors.New("dependency name must be a single chart name")
	}
	if relative, ok := strings.CutPrefix(repository, "file://"); ok {
		target := relative
		if !filepath.IsAbs(target) {
			target = filepath.Join(chartPath, filepath.FromSlash(relative))
		}
		resolved, err := filepath.EvalSymlinks(target)
		if err != nil {
			return "", errors.New("file:// dependency cannot be resolved")
		}
		root, err := filepath.EvalSymlinks(repositoryRoot)
		if err != nil || !withinRepository(root, resolved) {
			return "", errors.New("file:// dependency must stay inside the repository")
		}
		return "", nil
	}
	if !strings.HasPrefix(repository, "oci://") && !strings.HasPrefix(repository, "https://") {
		return "", errors.New("dependency repository must be an oci://, https://, or file:// URL")
	}
	reference := strings.TrimSuffix(repository, "/")
	if strings.HasPrefix(repository, "oci://") {
		reference += "/" + dependency.Name
	}
	if err := validateChartRepository(ctx, repository, reference, auths); err != nil {
		return "", err
	}
	return reference, nil
}

func dependencyVendored(chartPath, name string) bool {
	if name == "" || strings.ContainsAny(name, `/\*?[`) {
		return false
	}
	if info, err := os.Stat(filepath.Join(chartPath, "charts", name)); err == nil && info.IsDir() {
		return true
	}
	matches, _ := filepath.Glob(filepath.Join(chartPath, "charts", name+"-*.tgz"))
	return len(matches) > 0
}

// kustomizeRemoteChart is a remote helmCharts entry and the directory where
// Kustomize looks for an already downloaded copy.
type kustomizeRemoteChart struct {
	chart     core.HelmChartSource
	untarDir  string
	reference string
}

// prefetchKustomizeHelmCharts downloads remote helmCharts with JustCD's
// registry credentials into the location Kustomize checks before pulling.
// Kustomize sets its own HELM_CONFIG_HOME for the charts it pulls, so
// credentials cannot reach it any other way.
func prefetchKustomizeHelmCharts(ctx context.Context, charts []kustomizeRemoteChart, repositoryRoot string, auths []RegistryAuth) error {
	for _, remote := range charts {
		if info, err := os.Stat(filepath.Join(remote.untarDir, remote.chart.Chart)); err == nil && info.IsDir() {
			continue
		}
		if _, err := pullHelmChart(ctx, remote.chart, auths, repositoryRoot, remote.untarDir); err != nil {
			return fmt.Errorf("download Helm chart %q: %w", remote.chart.Chart, err)
		}
	}
	return nil
}
