# qarau

Online video transcription pipeline. Three Go components - `api` (gRPC + HTTP, sole DB owner),
`fetch` and `asr` (stateless workers that poll `api` over gRPC) - coordinated through a
Postgres job queue.

## Architecture

![qarau architecture](docs/qarau_mtls.png)

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
LD_LIBRARY_PATH=${PWD}/vosk-api/src:${PWD}/onnxruntime/lib ./bin/asr
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
```

## Fetch

Fetch worker has an alternative approach using JS runtime, see https://github.com/yt-dlp/yt-dlp/wiki/EJS

One needs to set `JS_RUNTIME_NAME` and `JS_RUNTIME_PATH` to enable it.

## mTLS: generating the CA and certs

Worker↔`api` gRPC traffic might need to be authenticated with mutual TLS instead of a shared token: each worker
gets its own client cert, `api` requires and verifies it, and the cert's SAN becomes
the worker's identity (used for `locked_by` / job-type authorization) instead of
trusting whatever the RPC payload claims.

Everything below is self-signed off one internal CA - there's no public CA involved,
since these names aren't publicly resolvable. Run from a throwaway `certs/` dir, `ca.key` never leaves the machine that generates it:

```sh
mkdir -p certs && cd certs

# 1. CA - the root of trust. ca.key stays offline; ca.crt is distributed to every host.
openssl genrsa -out ca.key 4096
openssl req -x509 -new -key ca.key -sha256 -days 3650 \
  -out ca.crt -subj "/CN=qarau internal CA"

# 2. api server cert - SAN must match the address workers actually dial.
openssl genrsa -out api.key 2048
openssl req -new -key api.key -out api.csr -subj "/CN=api"
openssl x509 -req -in api.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out api.crt -days 825 -sha256 \
  -extfile <(printf "subjectAltName=DNS:qarau.khairulin.com")

# 3. fetch client cert
openssl genrsa -out fetch.key 2048
openssl req -new -key fetch.key -out fetch.csr -subj "/CN=fetch_pi"
openssl x509 -req -in fetch.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out fetch.crt -days 825 -sha256 \
  -extfile <(printf "subjectAltName=URI:spiffe://qarau/fetch_pi")

# 4. asr client cert
openssl genrsa -out asr.key 2048
openssl req -new -key asr.key -out asr.csr -subj "/CN=asr_pod_01"
openssl x509 -req -in asr.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out asr.crt -days 825 -sha256 \
  -extfile <(printf "subjectAltName=URI:spiffe://qarau/asr_pod_01")
```

Distribute:

| Component  | Needs                              |
|------------|------------------------------------|
| `api`      | `ca.crt`, `api.crt`, `api.key`     |
| `asr`      | `ca.crt`, `asr.crt`, `asr.key`     |
| `fetch`    | `ca.crt`, `fetch.crt`, `fetch.key` |

`ca.crt` is the only thing that needs to be on all three - it's what each side uses
to verify the other's cert. Never copy `ca.key` off the machine that generated it;
losing it means every cert issued from it should be considered compromised.

825 days is an arbitrary rotation cadence (no automated renewal exists yet); reissue
leaf certs from the same `ca.crt`/`ca.key` before they expire.

## Lang ID

Export Torch model for ONNX Runtime

```python
$ uv venv
$ source .venv/bin/activate
$ uv pip --cache-dir ${PWD}/uv_cache install git+https://github.com/speechbrain/speechbrain.git@develop ipython <...>
$ ipython

from speechbrain.inference.classifiers import EncoderClassifier

language_id = EncoderClassifier.from_hparams(source="speechbrain/lang-id-voxlingua107-ecapa", savedir="tmp")

class LangIDWrapper(torch.nn.Module):
    def __init__(self, mods):
        super().__init__()
        self.compute_features = mods.compute_features   # Fbank (contains STFT)
        self.mean_var_norm    = mods.mean_var_norm      # per-utterance norm
        self.embedding_model  = mods.embedding_model    # ECAPA-TDNN
        self.classifier       = mods.classifier         # Linear + log_softm
    def forward(self, wavs):                    # wavs: [batch, time]
        wav_lens = torch.ones(wavs.shape[0], device=wavs.device)  # all full-length
        feats = self.compute_features(wavs)
        feats = self.mean_var_norm(feats, wav_lens)
        emb   = self.embedding_model(feats, wav_lens)
        return self.classifier(emb).squeeze(1)  # [batch, 107] log-pro

wrapper = LangIDWrapper(language_id.mods).eval()

dummy = torch.randn(1, 16000 * 10)              # 1 clip, 10 s @ 16 kHz

torch.onnx.export(
    wrapper,
    (dummy,),
    "voxlingua107_ecapa.onnx",
    input_names=["waveform"],
    output_names=["log_probs"],
    dynamic_axes={"waveform": {1: "time"}, "log_probs": {0: "batch"}},
    opset_version=18,                           # STFT needs >= 17
    do_constant_folding=True,
)

enc = language_id.hparams.label_encoder
labels = [enc.ind2lab[i] for i in range(len(enc.ind2lab))]  # expected 107 language labels in the format 'kk: Kazakh'
with open("voxlingua107_labels.json", "w") as f:
    json.dump(labels, f, ensure_ascii=False, indent=2)
```