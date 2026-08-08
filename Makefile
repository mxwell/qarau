MODULE    := github.com/mxwell/qarau
PROTO_DIR := proto
GEN_DIR   := gen

# go install drops binaries (protoc-gen-go, protoc-gen-go-grpc, sqlc, goose)
# into $GOBIN/$GOPATH/bin, which is not always on the shell PATH. Add it so
# protoc and friends can find the plugins.
GOBIN     := $(shell go env GOBIN)
GOPATH    := $(shell go env GOPATH)
export PATH := $(if $(GOBIN),$(GOBIN),$(GOPATH)/bin):$(PATH)

export CGO_CFLAGS="-I${PWD}/onnxruntime/include"
export CGO_LDFLAGS="-L${PWD}/onnxruntime/lib"

.PHONY: tools proto sqlc migrate-up migrate-down build run-api run-fetch run-asr test lint tidy

## tools: install codegen tooling (protoc plugins, sqlc, goose)
tools:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	go install github.com/pressly/goose/v3/cmd/goose@latest

## proto: generate protobuf + gRPC Go code into gen/
proto:
	protoc -I $(PROTO_DIR) \
		--go_out=$(GEN_DIR)      --go_opt=module=$(MODULE)/$(GEN_DIR) \
		--go-grpc_out=$(GEN_DIR) --go-grpc_opt=module=$(MODULE)/$(GEN_DIR) \
		$(PROTO_DIR)/qarau/v1/*.proto

## sqlc: generate type-safe DB code into db/gen/
sqlc:
	cd db && sqlc generate

## migrate-up: apply DB migrations (requires PG_DSN)
migrate-up:
	goose -dir db/migrations postgres "$(PG_DSN)" up

## migrate-down: roll back the most recent migration (requires PG_DSN)
migrate-down:
	goose -dir db/migrations postgres "$(PG_DSN)" down

## build: compile all components into bin/
build:
	go build -o bin/ ./cmd/...

fetch_build_arm:
	GOOS=linux GOARCH=arm64 go build -o bin/ ./cmd/fetch

run_pg_dev:
	docker run --rm --name postgres_dev -e POSTGRES_PASSWORD=admin -e POSTGRES_DB=qaraudb -p 5432:5432 -v postgres-data:/var/lib/postgresql -d postgres:18-bookworm

run_pg_client:
	psql -h localhost -p 5432 -U postgres -d qaraudb

run-api:
	go run ./cmd/api

run-fetch:
	go run ./cmd/fetch

run-asr:
	go run ./cmd/asr

test:
	LD_LIBRARY_PATH=${PWD}/vosk-api/src:${PWD}/onnxruntime/lib go test ./...

lint:
	go vet ./...

tidy:
	go mod tidy
