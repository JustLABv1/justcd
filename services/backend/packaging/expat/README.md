# Temporary Expat security package

Alpine 3.24 currently supplies Expat 2.8.5. JustCD builds the official Expat
2.9.0 source as an Alpine package to fix CVE-2026-77214 and CVE-2026-102633.
The APKBUILD is adapted from Alpine's 3.24-stable recipe, preserving its
configuration and library subpackage. No edge packages are used.

The source archive is pinned with SHA-512. Its SHA-256 was also checked against
the digest published for the official GitHub release asset:

```text
1e6371862cc31999b368c3b89b49994f0677e1bab5f1b2b85ae3741f5d803051
```

`abuild` checks the source checksum, builds the package and runs upstream
`make check`. A signing key is generated inside the build stage. Only the
public key and signed library package are copied to the runtime. The private
key is deleted before the build layer is saved; compilers, headers, tests and
static libraries stay in the build stage.
The runtime installs the signed package with normal APK signature verification
and requires `libexpat>=2.9.0-r0`. Package metadata reports the real installed
version to scanners.

CI builds the backend image and runs `scripts/check-backend-runtime.sh`, which
checks the installed version, resolves Git's Expat dependency and clones a
repository over HTTP as the image's unprivileged user.

Until this recipe is removed, maintain its version and checksum for subsequent
Expat security releases. Once Alpine 3.24 supplies a fixed package, remove the
`expat` Docker stage and its COPY instructions, install the official package
with the same minimum version requirement, and delete this directory. Rebuild,
run the runtime check and rescan the image digest with JFrog.
