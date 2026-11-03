# Supply an approved, pinned moby/buildkit rootless image when building.
# docker build --build-arg BUILDKIT_IMAGE=moby/buildkit:<approved-version>-rootless ...
ARG BUILDKIT_IMAGE
FROM ${BUILDKIT_IMAGE}
USER root
RUN apk add --no-cache git jq ca-certificates
# Install the platform CA certificates here before publishing your builder image.
USER 1000:1000
ENTRYPOINT []
