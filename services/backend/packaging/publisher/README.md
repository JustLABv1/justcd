# Publisher image maintenance

This Dockerfile adds Git and jq to a rootless BuildKit image. It does not certify
that an image meets a vulnerability policy. Scan the final image with the same
scanner and policy used by your registry before selecting it in a profile.

Build with a current, reviewed BuildKit rootless version or digest and refresh
cached layers:

```sh
docker build --pull --no-cache \
  --build-arg BUILDKIT_IMAGE=moby/buildkit:<version>-rootless \
  -t <registry>/justcd-publisher:<new-tag> \
  services/backend/packaging/publisher
```

The Dockerfile upgrades packages from the base image's configured Alpine
repositories. This can apply available OS fixes; it cannot fix vulnerabilities
compiled into BuildKit, runc, or other Go binaries. Those require patched upstream
binaries or an independently maintained compatible builder. An OS package upgrade
does not upgrade the Go runtime embedded in existing binaries.

Check installed and fixed versions and the final image digest in the scan report.
If no fix exists in the selected release/repositories, do not assume rebuilding
will resolve the finding. Do not disable scanner policies to make the image pass.

After the image passes your policy, push a new tag, preferably select its immutable
digest in the publishing profile, save a new profile version, and explicitly
upgrade/rebuild the application. Do not overwrite a tag already used by a pinned
profile version.
