# Installing gopedia with morphso

gopedia is an AI knowledge base service. morphso installs it and all required dependencies automatically.

## Prerequisites

- morphso installed → see [root README](../README.md#install)
- Docker (for `docker` strategy) or a Kubernetes cluster (for `k8s` strategy)

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

## What gets installed

gopedia depends on the following services, which morphso resolves and installs in order:

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

## Verify

After installation completes:

```sh
morphso list          # confirm gopedia appears in history
curl http://localhost:8787/health   # docker default port
```

## Uninstall

```sh
morphso remove gopedia
```

This removes gopedia. Dependencies shared with other packages are left intact.
