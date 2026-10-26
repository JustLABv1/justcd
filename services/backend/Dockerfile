FROM golang:1.27.3-bookworm AS toolchain
ENV GOTOOLCHAIN=local

FROM toolchain AS tools
WORKDIR /src/tools
COPY tools/go.mod tools/go.sum ./
RUN go mod download
RUN CGO_ENABLED=0 go build -mod=readonly -trimpath -ldflags="-s -w" -o /out/kustomize sigs.k8s.io/kustomize/kustomize/v5 \
    && CGO_ENABLED=0 go build -mod=readonly -trimpath -ldflags="-s -w -X helm.sh/helm/v4/internal/version.version=$(go list -m -f '{{.Version}}' helm.sh/helm/v4)" -o /out/helm helm.sh/helm/v4/cmd/helm

FROM toolchain AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/justcd-api .

# Build a signed Alpine package until the stable repository includes Expat 2.9.
FROM alpine:3.24.3 AS expat
RUN apk add --no-cache alpine-sdk
WORKDIR /src/expat
COPY packaging/expat/APKBUILD ./
RUN abuild-keygen -a -n \
    && cp /root/.abuild/*.rsa.pub /etc/apk/keys/ \
    && apk update \
    && abuild -F -r -P /packages \
    && mkdir -p /out/keys \
    && cp /root/.abuild/*.rsa.pub /out/keys/ \
    && find /packages -name 'libexpat-*.apk' -exec cp {} /out/ \; \
    && rm -f /root/.abuild/*.rsa

FROM alpine:3.24.3 AS runtime
COPY --from=expat /out/keys/ /etc/apk/keys/
COPY --from=expat /out/libexpat-*.apk /tmp/expat/
RUN apk upgrade --no-cache \
    && apk add --no-cache ca-certificates git openssh-client tini 'libexpat>=2.9.0-r0' /tmp/expat/*.apk \
    && rm -rf /tmp/expat
WORKDIR /app
COPY --from=build /out/justcd-api /usr/local/bin/justcd-api
COPY --from=tools /out/kustomize /usr/local/bin/kustomize
COPY --from=tools /out/helm /usr/local/bin/helm
ENV HOME=/tmp
USER 10001:10001
EXPOSE 8080
# Adopt and reap orphaned Git/renderer children; forward shutdown to the API.
ENTRYPOINT ["/sbin/tini", "--"]
CMD ["/usr/local/bin/justcd-api"]
