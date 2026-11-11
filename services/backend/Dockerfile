FROM golang:1.27.4-bookworm AS toolchain
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

FROM alpine:3.24.4 AS runtime
# Alpine 3.24 supplies the patched Expat; the minimum version keeps a stale mirror from installing an older one.
RUN apk upgrade --no-cache \
    && apk add --no-cache ca-certificates git openssh-client tini 'libexpat>=2.10.0-r0'
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
