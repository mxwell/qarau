# qarau

Online video transcription pipeline. Three Go components — `api` (gRPC + HTTP, sole DB owner),
`fetch` and `asr` (stateless workers that poll `api` over gRPC) — coordinated through a
Postgres job queue.

## Quick start (local)

```sh
make tools                 # one-time: install protoc plugins, sqlc, goose
make proto sqlc            # generate code

export PG_DSN=<..>
make migrate-up

make build                 # binaries in ./bin
```

## Layout

| Path        | Purpose                                              |
|-------------|------------------------------------------------------|
| `cmd/`      | Component entrypoints (`api`, `fetch`, `asr`).       |
| `proto/`    | Protobuf source of truth (`qarau/v1`).                  |
| `gen/`      | Generated protobuf + gRPC Go code (committed).       |
| `db/`       | Migrations, sqlc queries, generated DB code.         |
| `internal/` | Shared, non-public Go packages.                      |

## Chores

```sh
# clean ~/go/pkg/mod
go clean -modcache

# clean ~/.cache/go-build
go clean -cache
```