# Container security updates, 2026-10-12

The backend module already required Go 1.27.2, but its runtime also included
an independently built Kustomize and a downloaded Helm 4.3.0 binary. The latter
used Go 1.27.1, x/crypto 0.55.0 and x/net 0.57.0; Kustomize's temporary build
explicitly requested x/text 0.39.0. Updating the API module did not update those
tools. Published images are rebuilt only on release tags.

## Changes

- Build the API, agent, Helm and Kustomize using Go 1.27.2. Disable automatic
  toolchain switching in the container builds.
- Track Helm/Kustomize dependencies in `services/backend/tools/go.mod` and
  `go.sum`, including x/crypto 0.58.0, x/net 0.60.0 and x/text 0.43.0.
- Upgrade Alpine runtime packages during builds. Verified OpenSSL 3.5.9-r0
  and zlib 1.3.2-r1 in the resulting backend image.
- Update the frontend's transitive sharp dependency to 0.35.5, addressing
  [GHSA-wq5f-xc86-pv6w](https://github.com/advisories/GHSA-wq5f-xc86-pv6w).
- Check backend and tool source reachability with govulncheck in CI. Pull
  base images and bypass runtime-stage caches for releases. Save full Trivy
  reports and block publishing images with fixable HIGH/CRITICAL findings.

## Validation and remaining work

Backend tests passed using the rebuilt Helm/Kustomize. Source govulncheck scans
found no reachable vulnerabilities in the backend or tools. Frontend typecheck,
production container build and lint passed (22 existing lint warnings).
Workflow syntax passed actionlint. All three Linux ARM64 images built;
the four Go binaries also compiled for Linux AMD64.

Trivy 0.75.0 reports a conservative module-level GO-2026-5932 finding for
x/crypto in the API and Helm. Source analysis confirms that neither imports
the deprecated x/crypto/openpgp package; Helm uses the maintained ProtonMail
implementation. This advisory has no fixed module version.

Alpine 3.24 still supplies libexpat 2.8.5-r0, installed as a Git dependency
in the backend. Upstream fixes CVE-2026-77214 and CVE-2026-102633 in
[Expat 2.9.0](https://github.com/libexpat/libexpat/releases/tag/R_2_9_0).
The backend now builds and installs a signed `libexpat-2.9.0-r0.apk` from
checksum-pinned upstream source, using an APKBUILD adapted from Alpine 3.24.
The build runs upstream tests; CI verifies library resolution and a Git HTTP
clone as the unprivileged runtime user. Only the library package and public
signing key enter the runtime image. This removes the need to wait for Alpine
or mix stable and edge packages, but requires temporary downstream maintenance.

The downstream image built successfully on Linux ARM64. Upstream `make check`
passed, and the runtime check confirmed libexpat 2.9.0-r0, resolved Git's shared
library dependencies and successfully cloned the HTTP fixture. Trivy confirmed
the installed package version and reported only the two conservative
GO-2026-5932 module findings described above. The new CI job runs the same build
and runtime check on the Linux AMD64 GitHub runner.

## JFrog rescan reported by the user

The follow-up screenshots show three remaining findings and one policy
violation. The scanned image digest was not supplied.

- HIGH CVE-2026-77214 and MEDIUM CVE-2026-102633 both affect libexpat
  2.8.5-r0 and are fixed upstream in 2.9.0. Rechecked the Alpine 3.24 main
  package indexes for both x86_64 and aarch64: they still supply 2.8.5-r0.
  This prompted the temporary downstream Expat 2.9.0 package described above.
- LOW CVE-2026-106587 is reported against openssh-client-common 10.3_p1-r1.
  The [upstream fix](https://www.openssh.org/releasenotes.html#10.6) concerns
  `sshd` configuration options interpreted as filenames. The locally rebuilt
  backend image contains neither `sshd` nor the openssh-server package,
  so this server-specific issue does not apply to that image. The installed
  client package still produces a version-based scanner finding.

Publish and deploy the updated backend image, then rescan its exact digest
with JFrog to confirm that the two libexpat findings disappear. The release
Trivy check remains useful, but it currently misses these findings. Remove
the downstream packaging when Alpine 3.24 supplies a patched version; see
`services/backend/packaging/expat/README.md` for the removal procedure.
