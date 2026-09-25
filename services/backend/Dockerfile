FROM golang:1.27-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/justcd-api .
# Build the latest Kustomize release against the patched x/text module.
RUN mkdir /tmp/kustomize-build \
    && cd /tmp/kustomize-build \
    && go mod init justcd-kustomize-build \
    && go get sigs.k8s.io/kustomize/kustomize/v5@v5.8.1 golang.org/x/text@v0.39.0 \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/kustomize sigs.k8s.io/kustomize/kustomize/v5

FROM debian:bookworm-slim AS helm
ARG TARGETARCH=amd64
ARG HELM_VERSION=4.3.0
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/* \
    && curl -fsSL "https://get.helm.sh/helm-v${HELM_VERSION}-linux-${TARGETARCH}.tar.gz" -o /tmp/helm.tar.gz \
    && curl -fsSL "https://get.helm.sh/helm-v${HELM_VERSION}-linux-${TARGETARCH}.tar.gz.sha256sum" -o /tmp/helm.sha256sum \
    && echo "$(cut -d ' ' -f 1 /tmp/helm.sha256sum)  /tmp/helm.tar.gz" | sha256sum -c - \
    && mkdir /out \
    && tar -xzf /tmp/helm.tar.gz -C /out --strip-components=1 "linux-${TARGETARCH}/helm"

FROM alpine:3.24.2 AS runtime
RUN apk add --no-cache ca-certificates git openssh-client
WORKDIR /app
COPY --from=build /out/justcd-api /usr/local/bin/justcd-api
COPY --from=build /out/kustomize /usr/local/bin/kustomize
COPY --from=helm /out/helm /usr/local/bin/helm
ENV HOME=/tmp
USER 10001:10001
EXPOSE 8080
CMD ["/usr/local/bin/justcd-api"]
