// Package clusteragent runs inside a target cluster and makes outbound HTTPS
// requests only. Kubernetes authentication stays in this process.
package clusteragent

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/justlab/justcd/services/backend/internal/agentprotocol"
	"io"
	"k8s.io/client-go/rest"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Config struct {
	ClusterID           string                  `json:"clusterId,omitempty"`
	KubernetesServerURL string                  `json:"kubernetesServerUrl,omitempty"`
	ServerURL           string                  `json:"serverUrl"`
	KubernetesCAFile    string                  `json:"kubernetesCAFile,omitempty"`
	ServerCAFile        string                  `json:"serverCAFile,omitempty"`
	EnrollmentTokenFile string                  `json:"enrollmentTokenFile"`
	IdentityFile        string                  `json:"identityFile"`
	Profiles            []agentprotocol.Profile `json:"profiles"`
}
type Agent struct {
	Config        Config
	HTTP          *http.Client
	Local         map[string]*http.Client
	KubernetesURL string
	mu            sync.RWMutex
	identity      agentprotocol.Identity
}

func New(c Config, kube *rest.Config) (*Agent, error) {
	u, err := url.Parse(c.ServerURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("agent serverUrl must be an HTTPS URL")
	}
	if c.IdentityFile == "" || c.EnrollmentTokenFile == "" || len(c.Profiles) == 0 {
		return nil, errors.New("identity file, enrollment token file, and local profiles are required")
	}
	if len(c.ClusterID) > 128 || strings.TrimSpace(c.ClusterID) != c.ClusterID {
		return nil, errors.New("agent clusterId must be at most 128 bytes with no surrounding whitespace")
	}
	if c.KubernetesServerURL != "" {
		u, err := url.Parse(c.KubernetesServerURL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, errors.New("agent kubernetesServerUrl must be an HTTPS URL without credentials, query, or fragment")
		}
		kube = rest.CopyConfig(kube)
		kube.Host = strings.TrimRight(c.KubernetesServerURL, "/")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if c.ServerCAFile != "" {
		data, err := os.ReadFile(c.ServerCAFile)
		if err != nil {
			return nil, err
		}
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(data) {
			return nil, errors.New("invalid JustCD CA file")
		}
		transport.TLSClientConfig.RootCAs = pool
	}
	a := &Agent{Config: c, HTTP: &http.Client{Transport: transport, Timeout: 35 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, Local: map[string]*http.Client{}, KubernetesURL: strings.TrimRight(kube.Host, "/")}
	for _, p := range c.Profiles {
		if p.Name == "" || len(p.WorkspaceIDs) == 0 {
			return nil, errors.New("each local profile needs a name and explicit workspace IDs")
		}
		if _, ok := a.Local[p.Name]; ok {
			return nil, errors.New("duplicate local profile")
		}
		cfg := rest.CopyConfig(kube)
		if c.KubernetesCAFile != "" {
			cfg.TLSClientConfig.CAData = nil
			cfg.TLSClientConfig.CAFile = c.KubernetesCAFile
		}
		if p.TokenFile != "" {
			cfg.BearerToken = ""
			cfg.BearerTokenFile = p.TokenFile
			cfg.Username = ""
			cfg.Password = ""
			cfg.TLSClientConfig.CertData = nil
			cfg.TLSClientConfig.KeyData = nil
			cfg.TLSClientConfig.CertFile = ""
			cfg.TLSClientConfig.KeyFile = ""
		}
		cfg.Timeout = 25 * time.Second
		client, err := rest.HTTPClientFor(cfg)
		if err != nil {
			return nil, err
		}
		client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
		a.Local[p.Name] = client
	}
	return a, nil
}
func (a *Agent) call(ctx context.Context, method, path, token string, in, out any) (int, error) {
	var body []byte
	var err error
	if in != nil {
		body, err = json.Marshal(in)
		if err != nil {
			return 0, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(a.Config.ServerURL, "/")+"/api/v1/agents"+path, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := a.HTTP.Do(req)
	if err != nil {
		return 0, errors.New("JustCD connection failed")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return res.StatusCode, fmt.Errorf("JustCD returned HTTP %d", res.StatusCode)
	}
	if out != nil && res.StatusCode != 204 {
		err = json.NewDecoder(io.LimitReader(res.Body, 12<<20)).Decode(out)
	}
	return res.StatusCode, err
}
func (a *Agent) token() string { a.mu.RLock(); defer a.mu.RUnlock(); return a.identity.Token }
func (a *Agent) saveIdentity(i agentprotocol.Identity) error {
	data, err := json.Marshal(i)
	if err != nil {
		return err
	}
	dir := filepath.Dir(a.Config.IdentityFile)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".identity-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(name, a.Config.IdentityFile); err != nil {
		return err
	}
	a.mu.Lock()
	a.identity = i
	a.mu.Unlock()
	return nil
}
func (a *Agent) initialize(ctx context.Context) error {
	data, err := os.ReadFile(a.Config.IdentityFile)
	if err == nil {
		if err = json.Unmarshal(data, &a.identity); err != nil {
			return errors.New("invalid stored agent identity")
		}
		if a.identity.Token == "" {
			return errors.New("stored identity is empty")
		}
		uid, err := a.clusterUID(ctx)
		if err != nil {
			return err
		}
		if uid != a.identity.ClusterUID {
			return errors.New("stored agent identity does not match the configured or discovered cluster identity")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return a.enroll(ctx)
}
func (a *Agent) clusterUID(ctx context.Context) (string, error) {
	if a.Config.ClusterID != "" {
		return a.Config.ClusterID, nil
	}
	p := a.Config.Profiles[0]
	req, err := http.NewRequestWithContext(ctx, "GET", a.KubernetesURL+"/api/v1/namespaces/kube-system", nil)
	if err != nil {
		return "", err
	}
	res, err := a.Local[p.Name].Do(req)
	if err != nil {
		return "", fmt.Errorf("could not read target cluster identity: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", fmt.Errorf("could not read target cluster identity: Kubernetes returned HTTP %d (profile %q needs get permission for the kube-system namespace)", res.StatusCode, p.Name)
	}
	var ns struct {
		Metadata struct {
			UID string `json:"uid"`
		} `json:"metadata"`
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&ns); err != nil {
		return "", err
	}
	if ns.Metadata.UID == "" {
		return "", errors.New("target cluster UID is empty")
	}
	return ns.Metadata.UID, nil
}
func (a *Agent) enroll(ctx context.Context) error {
	uid, err := a.clusterUID(ctx)
	if err != nil {
		return err
	}
	enrollment, err := os.ReadFile(a.Config.EnrollmentTokenFile)
	if err != nil {
		return err
	}
	profiles := append([]agentprotocol.Profile(nil), a.Config.Profiles...)
	for i := range profiles {
		profiles[i].TokenFile = ""
	}
	var identity agentprotocol.Identity
	_, err = a.call(ctx, "POST", "/enroll", strings.TrimSpace(string(enrollment)), agentprotocol.Enrollment{ClusterUID: uid, Protocol: agentprotocol.Version, Version: "1.0.0", Profiles: profiles}, &identity)
	if err != nil {
		return err
	}
	if identity.ClusterUID != uid {
		return errors.New("enrollment identity has an unexpected cluster UID")
	}
	return a.saveIdentity(identity)
}
func (a *Agent) Execute(ctx context.Context, task agentprotocol.Request) agentprotocol.Result {
	result := agentprotocol.Result{Lease: task.Lease, ErrorCode: "cluster.agent_scope_denied"}
	var profile *agentprotocol.Profile
	for i := range a.Config.Profiles {
		if a.Config.Profiles[i].Name == task.Profile {
			profile = &a.Config.Profiles[i]
			break
		}
	}
	if profile == nil {
		result.Error = "local agent profile does not exist"
		slog.Warn("local agent profile denied request", "reason", result.Error, "profile", task.Profile, "workspace", task.WorkspaceID, "namespace", task.Namespace, "clusterScope", task.ClusterScope)
		return result
	}
	if err := agentprotocol.Validate(task, *profile); err != nil {
		result.Error = err.Error()
		slog.Warn("local agent profile denied request", "reason", result.Error, "profile", task.Profile, "workspace", task.WorkspaceID, "namespace", task.Namespace, "clusterScope", task.ClusterScope)
		return result
	}
	ctx, cancel := context.WithDeadline(ctx, task.Deadline)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, task.Method, a.KubernetesURL+task.URI, bytes.NewReader(task.Body))
	if err != nil {
		result.Error = "invalid local Kubernetes request"
		return result
	}
	req.Header.Set("Content-Type", task.ContentType)
	req.Header.Set("Accept", task.Accept)
	req.Header.Set("User-Agent", "JustCD-Agent/1.0")
	result.ErrorCode = ""
	res, err := a.Local[task.Profile].Do(req)
	if err != nil {
		result.ErrorCode = "cluster.agent_unknown_outcome"
		result.Error = "local Kubernetes request failed; execution outcome may be unknown"
		return result
	}
	defer res.Body.Close()
	result.Status = res.StatusCode
	result.ContentType = res.Header.Get("Content-Type")
	result.Body, err = io.ReadAll(io.LimitReader(res.Body, agentprotocol.MaxBody+1))
	if err != nil || len(result.Body) > agentprotocol.MaxBody {
		result.Body = nil
		result.ErrorCode = "cluster.agent_unknown_outcome"
		result.Error = "Kubernetes response exceeds the agent limit or could not be read"
	}
	return result
}
func (a *Agent) Run(ctx context.Context) error {
	if err := a.initialize(ctx); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	defer wg.Wait()
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			a.mu.RLock()
			expires := a.identity.ExpiresAt
			a.mu.RUnlock()
			if time.Until(expires) < 7*24*time.Hour {
				var i agentprotocol.Identity
				if _, err := a.call(ctx, "POST", "/renew", a.token(), nil, &i); err == nil {
					if err = a.saveIdentity(i); err != nil {
						slog.Error("could not persist renewed agent identity")
					}
				}
			}
			if _, err := a.call(ctx, "POST", "/heartbeat", a.token(), nil, nil); err != nil && ctx.Err() == nil {
				slog.Warn("agent heartbeat failed")
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	for ctx.Err() == nil {
		var task agentprotocol.Request
		status, err := a.call(ctx, "GET", "/tasks", a.token(), nil, &task)
		if err != nil {
			if status == 401 {
				if err := a.enroll(ctx); err == nil {
					continue
				}
				cancel()
				return errors.New("agent identity is expired or revoked; re-enroll this installation")
			}
			select {
			case <-ctx.Done():
			case <-time.After(2 * time.Second):
			}
			continue
		}
		if status == 204 {
			continue
		}
		result := a.Execute(ctx, task)
		// Retry delivery only. A leased Kubernetes operation is never executed twice.
		for ctx.Err() == nil && task.Deadline.After(time.Now()) {
			status, err = a.call(ctx, "POST", "/tasks/"+url.PathEscape(task.ID)+"/result", a.token(), result, nil)
			if err == nil || status == 410 {
				break
			}
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
		}
	}
	return ctx.Err()
}
