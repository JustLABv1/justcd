FROM golang:1.27.2-bookworm AS toolchain
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

FROM alpine:3.24.2 AS runtime
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates git openssh-client
WORKDIR /app
COPY --from=build /out/justcd-api /usr/local/bin/justcd-api
COPY --from=tools /out/kustomize /usr/local/bin/kustomize
COPY --from=tools /out/helm /usr/local/bin/helm
ENV HOME=/tmp
USER 10001:10001
EXPOSE 8080
CMD ["/usr/local/bin/justcd-api"]
