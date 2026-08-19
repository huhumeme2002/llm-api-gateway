# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -buildid=" -o /out/gateway ./cmd/gateway

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata wget \
    && addgroup -S -g 65532 llmgw \
    && adduser -S -u 65532 -G llmgw llmgw
WORKDIR /app
COPY --from=build /out/gateway /app/gateway
COPY config.example.yaml config.vps.yaml /app/
COPY data /app/data
ENV CONFIG_PATH=/app/config.yaml \
    TZ=UTC
COPY config.vps.yaml /app/config.yaml
EXPOSE 8080
USER 65532:65532
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=5 \
  CMD wget -qO- http://127.0.0.1:8080/health >/dev/null || exit 1
ENTRYPOINT ["/app/gateway"]
