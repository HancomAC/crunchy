# Crunchy

Crunchy is a deployment helper that builds the current project, publishes a Docker image, and updates one or more Cloud Run services.

## Features
- Runs the project build using the requested language/tooling (pnpm, npm, yarn, Go, or Rust) with auto-detection when not specified.
- Builds and pushes a `gcr.io/<project>/<image>` (or custom registry host) Docker image.
- Deploys the new image to multiple Cloud Run services in parallel and shifts traffic to the latest revision.
- Cleans up stale Cloud Run revisions and image digests with configurable retention limits.
- Infers the appropriate Container Registry host from the Cloud Run region when not specified.

## Installation

### Download pre-built binaries
Release artifacts are published for Linux (amd64) and macOS (arm64). You can fetch the latest release and run Crunchy in one shell command:

```sh
curl -sL https://github.com/HancomAC/crunchy/releases/latest/download/crunchy-linux-amd64 -o /tmp/crunchy && chmod +x /tmp/crunchy && /tmp/crunchy --help
```

Replace `linux-amd64` with `darwin-arm64` for Apple Silicon Macs. The binaries are named in the format `crunchy-<GOOS>-<GOARCH>`. Checksums are published alongside each binary as `<name>.sha256`.

### Build from source

```sh
git clone https://github.com/HancomAC/crunchy.git
cd crunchy/crunchy
go build -o crunchy .
```

## Usage

```
crunchy --image <name> \
        --svc <serviceA;serviceB;...> \
        --project <gcp-project> \
        --region <gcp-region> \
        [--beta] \
        [--keep-images <count>] \
        [--keep-revisions <count>] \
        [--lang <pnpm|npm|yarn|go|rust>]
```

### Required flags
- `--image`: Image name stored under `<registry-host>/<project>/`.
- `--svc`: Semicolon-delimited list of Cloud Run services to update.
- `--project`: Target GCP project.
- `--region`: Cloud Run region.

### Optional flags
- `--beta`: Tag the pushed image with `:dev` and skip cleanup of old image digests.
- `--keep-images`: Number of image digests to retain (default: `10`).
- `--keep-revisions`: Number of Cloud Run revisions to retain (default: `10`).
- `--registry-host`: Container registry host to target. Defaults to a region-specific host (e.g. `asia-northeast3` → `asia.gcr.io`, `europe-west1` → `eu.gcr.io`, `us-central1` → `us.gcr.io`, otherwise `gcr.io`).
- `--lang`: Manually select the project language/build tool. Accepted values: `pnpm`, `npm`, `yarn`, `go`, `rust`. Historical aliases like `node` still map to the pnpm build.

### Build tool detection
When `--lang` is not provided, Crunchy checks for well-known files in the working directory:
- `pnpm-lock.yaml`: run `pnpm run build`.
- `yarn.lock`: run `yarn build`.
- `package-lock.json`: run `npm run build`.
- `go.mod`: run `go build ./...`.
- `Cargo.toml`: run `cargo build --release`.
- `package.json` with none of the above locks: default to `pnpm run build` for backward compatibility.

If none of the indicators are present, Crunchy defaults to the Node (pnpm) build to preserve historical behavior.

### Example

```sh
crunchy \
  --image web-frontend \
  --svc prod-web;prod-api \
  --project my-project \
  --region asia-northeast3 \
  --keep-images 12 \
  --keep-revisions 15
```

## Requirements
- Matching tooling for your project (e.g. `pnpm`, `npm`, or `yarn` for Node/TypeScript builds; `go` for Go builds; `cargo` for Rust builds).
- Docker CLI with access to the inferred (or specified) registry host.
- Google Cloud SDK (`gcloud`) authenticated for the provided project and region.
