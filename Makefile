.PHONY: build test lint proto clean up down down-v logs ps tidy vet fmt sqlc sqlc-verify generate

VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X github.com/kmorozov/gophkeeper/internal/buildinfo.version=$(VERSION) \
	-X github.com/kmorozov/gophkeeper/internal/buildinfo.commit=$(COMMIT) \
	-X github.com/kmorozov/gophkeeper/internal/buildinfo.date=$(DATE)

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/gophkeeper-server ./cmd/server
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/gophkeeper-client ./cmd/client

test:
	go test -race ./...

lint:
	golangci-lint run

proto:
	docker run --rm -v "$$(pwd)":/workspace -w /workspace \
		golang:1.25-alpine sh -c '\
			apk add --no-cache protobuf-dev && \
			go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12 && \
			go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1 && \
			protoc -I proto \
				--go_out=proto --go_opt=paths=source_relative \
				--go-grpc_out=proto --go-grpc_opt=paths=source_relative \
				proto/gophkeeper/v1/gophkeeper.proto'

SQLC_VERSION ?= 1.30.0

sqlc:
	@command -v sqlc >/dev/null 2>&1 && sqlc generate || \
		docker run --rm -v "$$(pwd)":/src -w /src sqlc/sqlc:$(SQLC_VERSION) generate

sqlc-verify:
	@command -v sqlc >/dev/null 2>&1 && sqlc verify || \
		docker run --rm -v "$$(pwd)":/src -w /src sqlc/sqlc:$(SQLC_VERSION) verify

generate: proto sqlc
	@go mod tidy

clean:
	rm -rf bin

up:
	docker compose up -d --build

down:
	docker compose down

down-v:
	docker compose down -v

logs:
	docker compose logs -f server

ps:
	docker compose ps

tidy:
	go mod tidy

vet:
	go vet ./...

fmt:
	gofmt -w .
