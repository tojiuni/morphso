# morphso

AI service marketplace CLI — search, install, and manage AI-ready services with automatic strategy selection and dependency resolution.

> Default hub: **https://morphso.toji.homes**  
> To use your own hub: set `hub_url` in `~/.morphso/config.yaml`.

## Install

### One-liner (recommended)

```sh
curl -fsSL https://morphso.toji.homes/install | sh
```

Downloads a pre-built binary for your OS/arch (macOS/Linux, amd64/arm64). Falls back to `go install` if no binary is available, and installs Go automatically if needed. PATH is registered for bash/zsh/fish.

### Pre-built binary

Download the latest binary from [Releases](https://github.com/tojiuni/morphso/releases), extract, and place it in your PATH:

```sh
# Example for Linux amd64 — replace version and platform as needed
VERSION=$(curl -fsSL https://api.github.com/repos/tojiuni/morphso/releases/latest | grep '"tag_name"' | sed 's/.*"v\([^"]*\)".*/\1/')
curl -fsSL "https://github.com/tojiuni/morphso/releases/download/v${VERSION}/morphso_${VERSION}_linux_amd64.tar.gz" | tar -xz
sudo mv morphso /usr/local/bin/
```

### go install

```sh
go install github.com/tojiuni/morphso@latest
```

Requires Go 1.21+.

## Quick Start

```sh
morphso login                  # authenticate (ZITADEL Device Flow)
morphso search gopedia         # find packages
morphso info gopedia           # details + resource requirements
morphso install gopedia        # install — deps resolved automatically
morphso list                   # installation history
morphso remove gopedia         # uninstall
```

### Example: install gopedia (Docker)

The `--docker` strategy brings up a **self-contained Compose stack** — `gopedia` plus its
datastores (PostgreSQL, Qdrant, TypeDB) on one network. You do **not** provision the
datastores yourself; the stack does it, and the DB schema is auto-initialized on first start.

```sh
# OPENAI_API_KEY is required (embeddings for ingest + semantic search).
OPENAI_API_KEY=sk-... \
GOPEDIA_HTTP_PORT=18799 \
morphso install gopedia --docker --yes
```

After it finishes:

```sh
curl -s http://127.0.0.1:18799/api/health        # all deps ok
```

**Providing OPENAI_API_KEY** (pick one — the install warns if it is missing):

```sh
# 1) config template (recommended for --yes)
morphso install gopedia --docker --template       # writes ./gopedia.env from the hub template
#   edit gopedia.env → set OPENAI_API_KEY=sk-...
morphso install gopedia --docker --yes --config gopedia.env

# 2) environment variable
OPENAI_API_KEY=sk-... morphso install gopedia --docker --yes
```

Without a key, ingest runs but stores no vectors (Qdrant stays empty) and search fails.

**Useful env vars** (passed through to the generated Compose):

| Var | Default | Purpose |
|-----|---------|---------|
| `OPENAI_API_KEY` | — | **Required.** Embeddings for ingest + search. |
| `GOPEDIA_HTTP_PORT` | `8787` | Host port for the gopedia API. |
| `GOPEDIA_LOG_LEVEL` | `info` | `debug` for verbose logs. |
| `GOPEDIA_DB_AUTO_INIT` | `true` | Create the DB schema on start (idempotent). |
| `GOPEDIA_INGEST_DIR` | `~/.morphso/gopedia/ingest` | Host dir mounted at the container's `/ingest`. |

Ingest uses **container paths**: place files under `$GOPEDIA_INGEST_DIR` and ingest
`/ingest/<subdir>`. See [docs/example-gopedia.md](docs/example-gopedia.md) for k8s/native
strategies and full details.

## Commands

| Command | Description |
|---------|-------------|
| `login` / `logout` | Authenticate via ZITADEL Device Flow |
| `whoami` | Show the current authenticated user |
| `search <query>` | Search packages |
| `info <package>` | Package details, dependencies, resource summary |
| `install <package[@version]>` | Install with AI-recommended strategy; resolves deps |
| `remove <package>` | Uninstall a package |
| `list` | Show installation history (hub-tracked) |

## Configuration

`~/.morphso/config.yaml` is created on first login:

```yaml
hub_url: https://morphso.toji.homes  # override with --hub-url flag
token:   <set automatically by login>
```

Override for a single command: `morphso search foo --hub-url http://localhost:18080`

## Docs

- [Commands reference](docs/commands.md)
- [Install strategies](docs/install-strategies.md)
- [Dependency pipeline](docs/dependencies.md)
- [Configuration](docs/configuration.md)
- [Example: installing gopedia](docs/example-gopedia.md)
