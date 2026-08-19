GO ?= go
BIN ?= bin/gateway
CONFIG ?= config.yaml

.PHONY: run test race benchmark lint tidy build linux docker-up docker-down

run:
	$(GO) run ./cmd/gateway

build:
	$(GO) build -trimpath -ldflags="-s -w" -o $(BIN) ./cmd/gateway

linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags="-s -w" -o bin/gateway-linux-amd64 ./cmd/gateway
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags="-s -w" -o bin/gateway-linux-arm64 ./cmd/gateway

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

benchmark:
	$(GO) test -bench=. -benchmem ./...

lint:
	$(GO) vet ./...

tidy:
	$(GO) mod tidy

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down
