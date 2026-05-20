# gopedia_mcp Install Support: MCP Package Type (1st-class) — Design

- **Date**: 2026-05-20
- **Branches**: `feat/mcp-package-type` in both `morphso` and `morphso-hub`
- **Goal**: `moso install gopedia-mcp` 한 번으로 npm/docker 준비 + 감지된 MCP 클라이언트(Claude Code/Cursor/Gemini CLI)에 자동 등록까지 끝낸다. 다른 MCP 서버도 동일 패턴으로 추가 가능하도록 1급 타입으로 지원.

## Decisions (확정)

| 결정 | 선택 |
|---|---|
| 설치 범위 | MCP 타입 1급 지원 (CLI + hub 양쪽) |
| 등록 대상 클라이언트 | 감지된 것만 (Claude Code / Cursor / Gemini CLI 후보) |
| run 전략 | native(npm) + docker 둘 다 |
| env 처리 | 설치 시 prompt → MCP 클라이언트 config의 `mcpServers[name].env`에 주입 |
| OS 지원 v1 | darwin·linux 1급, windows best-effort (경로/override 구조만 마련) |

## Non-Goals

- `moso mcp list/remove` 별도 서브커맨드 — install/remove 플로우 통합으로 충분.
- 등록 충돌(동일 server_name 기존 존재) 자동 머지 정책 — confirm prompt + `--yes` 시 덮어쓰기로 충분.
- 임의의 MCP 클라이언트 동적 플러그인 시스템 — v1은 3개 하드코드.

## Architecture

```
사용자 → moso install gopedia-mcp
         │
         ├─ hub GET /packages/gopedia-mcp           → {type:"mcp", mcp_metadata:{...}}
         ├─ runDepsFlow (기존)                       → gopedia(required dep) 먼저 설치
         ├─ hub GET /packages/.../install-script    → npm install / docker pull
         ├─ runScriptFlow (기존)                     → 바이너리/이미지 준비
         └─ NEW: runMCPRegistration(pkg, s, reader)
                 ├─ env_schema 프롬프트 (--yes는 env/default fallback)
                 ├─ mcpclient.DetectInstalled()
                 └─ 감지된 각 클라이언트.Register(server_name, entry)
```

**책임 분리:**
- **hub**: 패키지 메타데이터 + 설치 스크립트(바이너리/이미지 준비)까지. 클라이언트 등록은 책임지지 않음.
- **CLI**: MCP 클라이언트 감지·등록·env 프롬프트. 클라이언트 종류 추가 시 CLI만 수정.
- **package author**: publish 시 `mcp_metadata`만 채우면 됨. 코드 변경 0.

## Hub Changes (morphso-hub)

### Schema

```sql
ALTER TABLE packages ADD COLUMN mcp_metadata JSONB;
-- type='mcp' 일 때만 NOT NULL은 애플리케이션 레벨에서 검증 (DB CHECK 제약은 v2 이후 데이터 안정화 후 추가)
```

마이그레이션 파일: `deploy/migrations/NNNN_add_mcp_metadata.up.sql` / `.down.sql`.

### mcp_metadata JSON 구조

```json
{
  "server_name": "gopedia",
  "native": {
    "command": "gopedia-mcp-server",
    "args": []
  },
  "docker": {
    "image": "artifacts.toji.homes/gopedia-mcp",
    "args": ["run", "-i", "--rm", "-e", "GOPEDIA_HOST_DOMAIN", "{{image}}"]
  },
  "platform_overrides": {
    "windows": {
      "native": { "command": "gopedia-mcp-server.cmd" }
    }
  },
  "env_schema": [
    {
      "name": "GOPEDIA_HOST_DOMAIN",
      "prompt": "Gopedia API host (예: 127.0.0.1:18787)",
      "required": true,
      "default": "127.0.0.1:18787",
      "secret": false
    }
  ]
}
```

- `{{image}}` 토큰은 CLI에서 `image:version` 으로 치환.
- `platform_overrides[GOOS]`는 base에 셸로우 머지 (native/docker 키별).
- `secret: true`면 prompt 시 입력 echo 끔.

### Code Changes

- `internal/domain/package.go`: `Package.MCPMetadata *MCPMetadata`. JSON Marshal/Unmarshal, JSONB scan/value 구현.
- `internal/registry/store.go`: 모든 SELECT/INSERT 쿼리에 `mcp_metadata` 컬럼 추가. NULL 처리.
- `internal/api/`:
  - POST/PUT `/packages` 핸들러에서 `mcp_metadata` 수신.
  - 검증: `type=mcp`이면 `mcp_metadata` 필수, `server_name`/`native or docker 최소 하나`/`env_schema[].name` 비어있지 않을 것. 누락 시 400 + 사람이 읽을 만한 에러.
  - GET `/packages/{slug}` 응답에 포함.
- `internal/installscript/llm.go`: mcp 타입 패키지의 install script 생성 프롬프트 분기. 결과 스크립트는 **바이너리/이미지 준비까지만** 생성 (클라이언트 등록 명령 포함 금지). 프롬프트 끝에 `# (MCP 클라이언트 등록은 morphso CLI가 담당)` 같은 주석 가이드 명시.

### Tests

- `registry/store_test.go`: `mcp_metadata` round-trip (Create → GetBySlug → assert deep equal).
- `api/*_test.go`:
  - POST type=mcp with valid metadata → 201, GET 응답 검증.
  - POST type=mcp without metadata → 400.
  - POST type=mcp with invalid metadata (server_name 비음, native/docker 양쪽 비음) → 400.
- `installscript/llm_test.go`: mcp 타입 시 프롬프트가 분기 변형을 포함하는지(스냅샷 또는 substring match).

## CLI Changes (morphso)

### New package: `internal/mcpclient/`

```
internal/mcpclient/
├── client.go         # 인터페이스 + DetectInstalled()
├── claude_code.go    # Claude Code 구현
├── cursor.go         # Cursor 구현
├── gemini_cli.go     # Gemini CLI 구현
└── *_test.go
```

**Interface:**
```go
type MCPClient interface {
    Name() string                                              // "claude-code", "cursor", "gemini-cli"
    Detected() bool                                            // config 파일 경로 존재 여부
    Register(serverName string, entry MCPServerEntry) error    // mcpServers에 머지 (atomic write)
    Unregister(serverName string) error                        // mcpServers에서 제거 (idempotent)
}

type MCPServerEntry struct {
    Command string
    Args    []string
    Env     map[string]string
}

func DetectInstalled() []MCPClient   // 등록된 모든 클라이언트 중 Detected()=true 인 것만
```

**Config 경로 (`configPath() string` 각 구현):**

| 클라이언트 | darwin | linux | windows |
|---|---|---|---|
| Claude Code | `~/Library/Application Support/Claude/claude_desktop_config.json` | `~/.config/Claude/claude_desktop_config.json` | `%APPDATA%\Claude\claude_desktop_config.json` |
| Cursor | `~/.cursor/mcp.json` | `~/.cursor/mcp.json` | `%USERPROFILE%\.cursor\mcp.json` |
| Gemini CLI | `~/.gemini/settings.json` | `~/.gemini/settings.json` | `%USERPROFILE%\.gemini\settings.json` |

**쓰기 안전성:**
1. config 파일 읽기 → JSON unmarshal (없으면 빈 객체에서 시작).
2. `mcpServers[serverName] = {command, args, env}` 머지. 다른 키는 보존.
3. `.bak` 파일 생성 → tmp 파일에 marshal → `os.Rename(tmp, target)` (atomic).
4. 권한: 0600 유지 (secret env가 들어갈 수 있음).

### hub client types (`internal/hub/types.go`, `parse.go`)

`Package` JSON unmarshal에 `MCPMetadata *MCPMetadata` 필드 추가. 새 타입:

```go
type MCPMetadata struct {
    ServerName        string                    `json:"server_name"`
    Native            *MCPCommandSpec           `json:"native,omitempty"`
    Docker            *MCPCommandSpec           `json:"docker,omitempty"`
    PlatformOverrides map[string]MCPOverride    `json:"platform_overrides,omitempty"`
    EnvSchema         []MCPEnvSpec              `json:"env_schema,omitempty"`
}
type MCPCommandSpec struct {
    Command string   `json:"command,omitempty"`
    Image   string   `json:"image,omitempty"`
    Args    []string `json:"args,omitempty"`
}
type MCPOverride struct {
    Native *MCPCommandSpec `json:"native,omitempty"`
    Docker *MCPCommandSpec `json:"docker,omitempty"`
}
type MCPEnvSpec struct {
    Name     string `json:"name"`
    Prompt   string `json:"prompt"`
    Required bool   `json:"required"`
    Default  string `json:"default,omitempty"`
    Secret   bool   `json:"secret,omitempty"`
}
```

### `cmd/install.go`: runMCPRegistration

`runInstall` 끝부분, hub install script 성공 후 `RecordInstall` 직전에 hook:

```go
if pkg.Type == "mcp" {
    if err := runMCPRegistration(pkg, s, strategy, version, stdinReader); err != nil {
        // 등록 실패해도 바이너리/이미지는 준비 완료 → 경고만, 수동 등록 가이드 출력
        fmt.Printf("⚠ MCP 클라이언트 자동 등록 실패: %v\n", err)
        printManualRegisterHint(pkg)
    }
}
```

`runMCPRegistration` 흐름:
1. `pkg.MCPMetadata` nil 검증 (hub 응답 이상 → error).
2. `mergeOverride(meta, runtime.GOOS)` — base + GOOS override 머지.
3. env_schema 순회:
   - `--yes`: `os.Getenv(name)` → 없으면 `Default` → required면서 둘 다 비면 error.
   - 일반: prompt 출력 (default 표시 `[기본: xxx]`), secret이면 echo off. 빈 입력 → default 사용.
4. strategy(native/docker)에 따라 `MCPServerEntry` 구성:
   - native: `{Command: meta.Native.Command, Args: meta.Native.Args, Env: collected}`.
   - docker: `{Command: "docker", Args: meta.Docker.Args with "{{image}}" → "image:version" 치환, Env: collected}`.
5. `mcpclient.DetectInstalled()` → 각 클라이언트.Register. 결과 라인 출력:
   - `✓ claude-code에 'gopedia' 등록`
   - `· cursor: 미감지, skip`
   - `✗ gemini-cli: 등록 실패: ...`

### `cmd/remove.go`: Unregister 통합

remove 흐름에서 type=mcp인 패키지일 때:
- `pkg.MCPMetadata.ServerName` 기준으로 `mcpclient.DetectInstalled()` 각각에 `Unregister(name)`.
- 등록 안 됐어도 조용히 skip (idempotent).

### `internal/installer/installer.go`

- `buildNative`의 `case "mcp"`: `npm install -g <slug>@<version>` 로 fallback (hub script 없을 때 한정).
- docker strategy의 기본 `docker run -d --name`은 mcp 타입에서 의미 없음(stdio라 daemon 아님). 그러나 BuildCommand는 fallback 경로일 뿐이고 mcp 패키지는 hub install script가 존재하는 게 정상 → fallback 분기에서 mcp 타입은 명시적 에러 또는 npm으로만 처리. **방침: fallback 시 mcp+docker → 명시적 에러 ("mcp 타입은 hub install script 필요")**.

### Recommendation (`internal/hub/deps.go` LocalRecommend)

- 입력 `pkg.Type == "mcp"`일 때:
  - `npm` 가용 + Node ≥ 18 가정 시 → `native` 추천.
  - 아니면 docker 가용 시 → `docker`.
  - 둘 다 없음 → 에러.

### Tests

- `mcpclient/*_test.go`: 임시 `$HOME` 픽스처, 각 클라이언트마다:
  - Detected: config 파일 없음/있음 케이스.
  - Register: 기존 mcpServers 보존, 새 항목 추가, 동일 이름 덮어쓰기.
  - Unregister: 존재/미존재 idempotent.
  - 다른 최상위 키 보존 (`theme`, `tools` 등 임의 키 라운드트립).
  - atomic write: 중간 실패 시 원본 보존 (마운트된 readonly로 시뮬 또는 rename 실패 주입).
- `cmd/install.go`: runMCPRegistration 단위 테스트 (mcpclient 인터페이스 스파이로):
  - env_schema prompt 흐름 (입력 stub).
  - `--yes` 모드 env/default fallback.
  - 누락 required env → 에러.
  - `{{image}}` 치환.
  - GOOS override 머지.

## Data: gopedia-mcp Package Registration

마이그레이션·코드 머지 후 publish:

```yaml
name: Gopedia MCP Server
slug: gopedia-mcp
type: mcp
description: Gopedia HTTP API를 MCP 도구로 노출하는 stdio 서버
recommended_strategy: native
tags: [mcp, gopedia, search]
mcp_metadata:
  server_name: gopedia
  native:
    command: gopedia-mcp-server
    args: []
  docker:
    image: artifacts.toji.homes/gopedia-mcp
    args: ["run", "-i", "--rm", "-e", "GOPEDIA_HOST_DOMAIN", "{{image}}"]
  env_schema:
    - name: GOPEDIA_HOST_DOMAIN
      prompt: "Gopedia API host (예: 127.0.0.1:18787)"
      required: true
      default: "127.0.0.1:18787"
dependencies:
  - slug: gopedia
    min_version: ""
    optional: false
```

- gopedia를 required dep로 두면 기존 `runDepsFlow`가 먼저 설치 (or skip if installed).
- install script (hub LLM 자동 생성 후 검토): hub의 install script는 (slug, version, strategy) 단위로 저장되므로 version은 저장 시점에 hub가 스크립트 본문에 박는다. CLI 측 추가 env 주입 불필요.
  - native: `npm install -g gopedia-mcp-server@<version-or-latest>`
  - docker: `docker pull artifacts.toji.homes/gopedia-mcp:<version-or-latest>`

## Migration / Rollout

**Worktree:**
```
~/Documents/dev/morphso-wt-mcp-type           (브랜치 feat/mcp-package-type)
~/Documents/dev/morphso-hub-wt-mcp-type       (브랜치 feat/mcp-package-type)
```

**머지 순서:**
1. hub PR 머지 → 마이그레이션 적용 → 배포.
2. CLI PR 머지 → 릴리즈.
3. gopedia-mcp 패키지 데이터 publish + install script 등록.
4. E2E 검증: 깨끗한 환경에서 `moso install gopedia-mcp` → Claude Code config에 등록 확인 → 실제 MCP 도구 호출.

**롤백 안전성:**
- hub: 컬럼 추가만 — 기존 패키지 무영향. type≠mcp 흐름 변화 없음.
- CLI: type=mcp 분기에만 신규 동작.
- 등록 실패 시 install 자체는 성공 유지(바이너리/이미지 준비됨). 수동 등록 가이드 출력.
- 모든 client config 쓰기는 `.bak` 백업 + atomic rename.

## Open Questions (구현 시 결정 — 차단 아님)

- Claude Code의 정확한 config 파일명/경로 (`claude_desktop_config.json` vs `~/.claude/...`): 구현 시 실측 후 확정.
- Cursor mcp 등록의 정확한 JSON 스키마(현재 문서 부족): 실측 후 확정.
- Gemini CLI `mcpServers` 키 명세: 실측 후 확정.

위 세 가지는 구현 단계에서 각 클라이언트 실측으로 결정 — 설계 자체의 변경은 없음 (configPath와 JSON 머지 로직만 영향).
