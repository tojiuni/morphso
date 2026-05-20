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

## Installing the MCP Server Together (gopedia + gopedia-mcp)

`gopedia` lists `gopedia-mcp` as an **optional dependency**, so installing gopedia will prompt you to also install its MCP server and register it with detected MCP clients (Claude Code, Cursor, Gemini CLI).

```sh
moso install gopedia --strategy=native
```

You'll see the optional dependency prompt:

```
[optional] Gopedia MCP Server — LLM 설정을 선택하세요:
  [1] Gopedia MCP Server 신규 설치 (Docker)
  [2] 기존 Gopedia MCP Server URL 입력
  [3] API 토큰 입력 (OpenAI / Anthropic / 기타)
  [4] 건너뜀
선택 [1-4]:
```

Select `[1]` to install gopedia-mcp. After the install script runs, you'll be prompted for the gopedia API host (defaults to `127.0.0.1:18787`) and the MCP server will be registered in every detected client config:

```
Gopedia API host (예: 127.0.0.1:18787) [기본: 127.0.0.1:18787]:
✓ claude-code에 'gopedia' 등록
✓ cursor에 'gopedia' 등록
✓ gemini-cli에 'gopedia' 등록
```

The MCP server is now available in your IDE/CLI immediately.

### Install gopedia-mcp Directly

If you only want the MCP integration without gopedia itself (e.g., gopedia is already running elsewhere), install it standalone:

```sh
moso install gopedia-mcp
```

The same MCP registration flow runs, prompting for the gopedia host URL and registering with detected clients.

### Skip the Optional MCP Install

If you don't want the MCP integration, select `[4]` at the prompt — gopedia installs without it.

> **Note:** The optional-dep prompt currently uses LLM-configuration phrasing (a holdover from the original Ollama use case). Selecting `[1]` works correctly for any optional dependency type; options `[2]`/`[3]` are only meaningful for LLM packages.
