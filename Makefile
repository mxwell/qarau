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

ONNXRUNTIME_VERSION := 1.27.1
VOSK_API_COMMIT     := 72797111dba20cd7e32c4ede867a4b81dccb0708

.PHONY: tools deps deps-vosk proto sqlc migrate-up migrate-down build build-vosk run-api run-fetch run-asr test test-vosk lint lint-vosk tidy

## tools: install codegen tooling (protoc plugins, sqlc, goose)
tools:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	go install github.com/pressly/goose/v3/cmd/goose@latest

## deps: fetch onnxruntime, the native lib needed for VAD/LangID
deps:
	@if [ ! -f onnxruntime/lib/libonnxruntime.so ]; then \
		echo "Fetching onnxruntime $(ONNXRUNTIME_VERSION)..."; \
		curl -fsSL -o /tmp/onnxruntime.tgz \
			https://github.com/microsoft/onnxruntime/releases/download/v$(ONNXRUNTIME_VERSION)/onnxruntime-linux-x64-$(ONNXRUNTIME_VERSION).tgz; \
		rm -rf onnxruntime && mkdir onnxruntime; \
		tar -xzf /tmp/onnxruntime.tgz -C onnxruntime --strip-components=1; \
		rm /tmp/onnxruntime.tgz; \
	fi

## deps-vosk: fetch vosk-api sources + prebuilt libvosk.so, only needed for the Vosk transcriber
## We don't want to build the whole Kaldi & friends stuff,
## so extract the *.so from the Python package
deps-vosk:
	@if [ ! -f vosk-api/src/libvosk.so ]; then \
		echo "Fetching vosk-api @ $(VOSK_API_COMMIT) + prebuilt libvosk.so..."; \
		rm -rf vosk-api; \
		git clone --quiet https://github.com/alphacep/vosk-api.git vosk-api; \
		git -C vosk-api checkout --quiet $(VOSK_API_COMMIT); \
		pip3 install --quiet --no-input --target /tmp/vosk-pip vosk; \
		cp /tmp/vosk-pip/vosk/libvosk.so vosk-api/src/libvosk.so; \
		rm -rf /tmp/vosk-pip; \
	fi

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

## build: compile all components into bin/ (without Vosk)
build:
	go build -o bin/ ./cmd/...

## build-vosk: same, with the legacy Vosk transcriber compiled in
build-vosk:
	go build -tags vosk -o bin/ ./cmd/...

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
	LD_LIBRARY_PATH=${PWD}/onnxruntime/lib go test ./...

## test-vosk: run tests with the legacy Vosk transcriber compiled in
test-vosk:
	LD_LIBRARY_PATH=${PWD}/vosk-api/src:${PWD}/onnxruntime/lib go test -tags vosk ./...

lint:
	go vet ./...

## lint-vosk: vet the Vosk-tagged files too
lint-vosk:
	go vet -tags vosk ./...

tidy:
	go mod tidy
