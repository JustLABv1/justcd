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

## Remaining upstream findings (2026-10-12)

- The module-wide GO-2026-5932 advisory for deprecated
  `golang.org/x/crypto/openpgp` has no fixed version. A stripped-binary scan
  reports it conservatively; check source reachability before treating this
  as an application finding. The API does not import that package, and Helm
  uses `github.com/ProtonMail/go-crypto/openpgp` instead.
- Alpine 3.24 currently supplies libexpat 2.8.5-r0, which does not fix
  CVE-2026-77214. Git pulls this library into the backend runtime. A patched
  Alpine package is still needed. Do not mix Alpine stable and edge packages
  or suppress this finding. Trivy's current Alpine database does not report
  this CVE yet; a passing Trivy scan does not mean it has been resolved.

Release builds pull base images, bypass cached package installation, and run
`apk upgrade` to obtain available distribution fixes. Trivy blocks publishing
for HIGH/CRITICAL vulnerabilities with fixes available in its database;
unfixed findings remain an explicit follow-up and are not a clean bill of health.
Existing published images require a new release and deployment to receive fixes.
