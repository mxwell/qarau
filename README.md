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

## Extract audio_blob

```
psql -h localhost -p 5432 -U postgres -d qaraudb < extract_audio_blog.sql | xxd -r -p | tail -c +2 > f8Adt8gBxBw.webm
# password prompt here
```

There is an extra byte at the start, that is removed by `tail`. The cause is not clear.

## ASR worker

```
LD_LIBRARY_PATH=$PWD/vosk-api/src ./bin/asr
```

## Init prod DB

```
sudo -u postgres psql
CREATE USER qarau WITH PASSWORD '***secret***';
CREATE DATABASE qaraudb OWNER qarau;

scp goose qarau.khairulin.com:/qarau-bundle/db-setup/goose
scp -r db/migrations qarau.khairulin.com:/qarau-bundle/db-setup/migrations

export DB_URL=postgres://qarau:***secret***@localhost:5432/qaraudb?sslmode=disable
./goose -dir migrations postgres $DB_URL up