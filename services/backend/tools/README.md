# Runtime tools

This module locks Helm and Kustomize independently of the API dependencies.
The backend Dockerfile builds all three binaries with the same patched Go
toolchain. Downloading official Helm binaries would retain the compiler and
dependencies used by the upstream release, regardless of the API's go.mod.

Renovate can update these dependencies using its existing Go module rules.
To update manually, run from this directory:

```sh
go get -tool helm.sh/helm/v4/cmd/helm@VERSION sigs.k8s.io/kustomize/kustomize/v5@VERSION
go get golang.org/x/crypto@VERSION golang.org/x/net@VERSION golang.org/x/text@VERSION
go mod tidy
```

After changing dependencies, build both binaries, scan them with
`govulncheck -mode=binary`, and run the backend tests with the rebuilt tools
on PATH. Preserve the Helm version using the linker flag in the Dockerfile.

The `kube-openapi` replacement keeps Kustomize 5.8.3 compatible with Helm's
Kubernetes 0.37 dependencies in this shared module. Kustomize selects a newer
revision whose schema types use `structured-merge-diff/v7`, while Kubernetes
0.37 expects v6. Remove the replacement once both tools use compatible schema
types, and rebuild both binaries before doing so.

## Xray dependency fixes (2026-10-19)

- XRAY-1117186: the bundled Helm binary now selects ORAS v2.6.4, newer
  than Xray's minimum fix v2.6.4-0.20261015110029-8883c8588eb5.
- XRAY-1110429 (GHSA-mgcc-cww5-fp2q): both Go modules pin go-jose
  v4.1.6. The API uses it through OIDC; the tools module also retains it
  through Helm's dependency tests. This release caps parsed RSA keys at
  4096 bits by default, so OIDC providers using larger keys need review.

Rebuild and publish the backend image, deploy it, and rescan its new digest
in Xray to confirm these findings are cleared. Updating the API module alone
does not update ORAS in the separately built Helm binary.

## Remaining upstream findings (2026-10-12)

- The module-wide GO-2026-5932 advisory for deprecated
  `golang.org/x/crypto/openpgp` has no fixed version. A stripped-binary scan
  reports it conservatively; check source reachability before treating this
  as an application finding. The API does not import that package, and Helm
  uses `github.com/ProtonMail/go-crypto/openpgp` instead.
- Alpine 3.24 currently supplies libexpat 2.8.5-r0, which does not fix
  CVE-2026-77214 or CVE-2026-102633. The backend now builds a signed Expat
  2.9.0 package from upstream source using `packaging/expat/APKBUILD`.
  See `packaging/expat/README.md` for maintenance and removal instructions.
  Trivy's current Alpine database misses these CVEs, so verify the installed
  package version and confirm the resulting image digest with JFrog.

Release builds pull base images, bypass cached package installation, and run
`apk upgrade` to obtain available distribution fixes. Trivy blocks publishing
for HIGH/CRITICAL vulnerabilities with fixes available in its database;
unfixed findings remain an explicit follow-up and are not a clean bill of health.
Existing published images require a new release and deployment to receive fixes.
