# Install Strategies

morphso supports four strategies for installing packages. When you run `morphso install <package>` without specifying a strategy, the hub queries an LLM with your OS and architecture to recommend the best fit.

## Strategies

| Strategy | Tool | Best For |
|----------|------|----------|
| `native` | system package manager (brew, apt, …) or binary download | CLI tools, single binaries |
| `docker` | `docker run` | Stateful services, multi-component apps |
| `k8s` | `kubectl apply` | Services deployed to a Kubernetes cluster |
| `helm` | `helm install` | Kubernetes services distributed as Helm charts |

## AI Recommendation

When no `--strategy` flag is given, morphso asks the hub to recommend a strategy. The hub passes your OS and architecture (`MOSO_OS`, `MOSO_ARCH`) to a language model and returns:

- **Strategy** — one of `native`, `docker`, `k8s`, `helm`
- **Reason** — a short explanation shown before install proceeds

```
Recommended strategy: docker
Reason: gopedia is a stateful service with multiple components — Docker is the
        lowest-friction option on linux/amd64 without a cluster available.
```

You are shown the recommendation and can confirm or cancel before anything is installed.

## Overriding the Strategy

Use a flag to skip the recommendation and force a specific strategy:

```sh
morphso install gopedia --docker
morphso install gopedia --strategy=k8s
```

## Install Scripts

Each package + strategy combination is backed by a shell script stored in morphso-hub. The script is fetched over HTTPS, its SHA-256 hash is verified, and then executed locally. Scripts receive environment variables populated by resolved dependencies (e.g., `POSTGRES_HOST`, `QDRANT_PORT`, `REDIS_HOST`).
