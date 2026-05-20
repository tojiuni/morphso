# Installing gopedia with morphso

gopedia is an AI knowledge base service. By default morphso installs it **and** all required dependencies automatically. You can also install gopedia alone and point it at services you already run (`--no-deps` + a config file) — see [Install against existing services](#install-against-existing-services).

## Prerequisites

- morphso installed → see [root README](../README.md#install)
- Docker (for `docker` strategy) or a Kubernetes cluster (for `k8s` strategy)
- For `--no-deps`: reachable PostgreSQL, Qdrant, TypeDB, and (for retrieval/generation) an embedding provider and an LLM endpoint such as Ollama or OpenAI

## Quick install

```sh
morphso install gopedia
```

morphso fetches an AI-generated install script from the hub and runs it. The strategy is selected automatically based on your environment (defaults to `docker`).

## Choose a strategy

```sh
morphso install gopedia --strategy docker    # Docker Compose (default)
morphso install gopedia --strategy k8s       # Kubernetes manifests
morphso install gopedia --strategy native    # bare-metal / local binaries
```

Useful flags:

```sh
morphso install gopedia --yes                # non-interactive (CI / scripts)
morphso install gopedia --template           # write the config template to ./gopedia.env and exit
morphso install gopedia --config ./gopedia.env   # inject a config file (exposed to the script as MOSO_CONFIG)
morphso install gopedia --no-deps            # install gopedia only; do not install dependencies
```

## What gets installed

By default gopedia pulls in the following services, which morphso resolves and installs in order:

| Dependency | Purpose |
|------------|---------|
| PostgreSQL | primary relational store |
| Qdrant | vector search |
| Redis | cache / pub-sub |
| TypeDB | knowledge graph |

You can inspect the full dependency tree before installing:

```sh
morphso info gopedia
```

> **Note:** the LLM and embedding providers (e.g. Ollama / OpenAI) are **not** in this dependency list — gopedia reads them from environment/config at runtime. Provide them via `--config` (see below).

## Install against existing services

If you already run PostgreSQL, Qdrant, TypeDB, and an embedding/LLM endpoint, install gopedia alone with `--no-deps` and point it at them through a config file. The install script sources the file from `MOSO_CONFIG`, so any variable you set there is exported into gopedia's environment.

```sh
# 1. Get the config template (lists the variables gopedia understands)
morphso install gopedia --template          # writes ./gopedia.env

# 2. Edit ./gopedia.env to point at your services (example)
cat > gopedia.env <<'EOF'
# HTTP
GOPEDIA_HTTP_ADDR=0.0.0.0:8787

# PostgreSQL
POSTGRES_HOST=postgres.example.svc
POSTGRES_PORT=5432
POSTGRES_DB=gopedia
POSTGRES_USER=gopedia
POSTGRES_PASSWORD=...
POSTGRES_SSLMODE=disable

# Qdrant
QDRANT_HOST=qdrant.example.svc
QDRANT_PORT=6333
QDRANT_GRPC_PORT=6334
QDRANT_API_KEY=...

# TypeDB
TYPEDB_HOST=typedb.example.svc
TYPEDB_PORT=1729
TYPEDB_DATABASE=gopedia

# LLM (Ollama) and embeddings — pick what you run
OLLAMA_CHAT_URL=http://ollama.example.svc:11434
OLLAMA_CHAT_MODEL=gemma2:27b
OPENAI_API_KEY=...           # if embeddings/generation use OpenAI
EOF

# 3. Install gopedia only, against those services
morphso install gopedia --no-deps --config ./gopedia.env --yes
```

This is the recommended path for CI and for shared clusters where the data services already exist.

`--no-deps` and `--config` are **strategy-independent** — the same approach works for `--strategy docker`, `--strategy native`, and `--strategy k8s`. In every case morphso installs gopedia alone and gopedia connects to the services named in your config.

## Verify

After installation completes:

```sh
morphso list                                  # confirm gopedia appears in history
curl http://localhost:8787/api/health         # liveness (default GOPEDIA_HTTP_ADDR=…:8787)
curl http://localhost:8787/api/health/deps    # dependency connectivity (PostgreSQL/Qdrant/TypeDB/embedding)
```

`/api/health/deps` reports the status of each backing service, so it is the quickest way to confirm a `--no-deps` install reached your existing PostgreSQL, Qdrant, TypeDB, and embedding/LLM endpoints.

## Uninstall

```sh
morphso remove gopedia
```

This removes gopedia. Dependencies shared with other packages are left intact.
