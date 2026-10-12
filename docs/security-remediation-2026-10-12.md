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

**CVE-2026-77214 remains open:** Alpine 3.24 still supplies libexpat 2.8.5-r0,
installed as a Git dependency in the backend. Upstream fixes it in
[Expat 2.9.0](https://github.com/libexpat/libexpat/releases/tag/R_2_9_0).
The current Trivy Alpine database does not report this CVE, so its absence
from that scan is not evidence of remediation. Rebuild after Alpine ships
the fix and verify with the original JFrog scanner.

These changes have not been published or deployed. A new release, deployment
and JFrog rescan of the resulting image digest are needed to update the running
installation and reconcile the original 25 findings.
