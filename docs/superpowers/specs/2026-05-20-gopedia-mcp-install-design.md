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
| env 처리 | 설치 시 prompt → MCP 클라이언트 config의 `env`에 주입 (key 경로는 클라이언트별) |
| OS 지원 v1 | darwin·linux 1급, windows 미지원 (경로/override 구조만 마련하되 v1에서는 registration skip + 명시적 메시지) |

## Non-Goals

- `moso mcp list/remove` 별도 서브커맨드 — install/remove 플로우 통합으로 충분.
- 등록 충돌(동일 server_name 기존 존재) 자동 머지 정책 — "기존 env를 default로 재제시 + 머지" 로 충분 (I1 참조).
- 임의의 MCP 클라이언트 동적 플러그인 시스템 — v1은 3개 하드코드.
- Claude Desktop(Electron 앱) 지원 — Claude **Code** CLI만 대상.

## Architecture

```
사용자 → moso install gopedia-mcp
         │
         ├─ hub GET /packages/gopedia-mcp           → {type:"mcp", mcp_metadata:{...}}
         ├─ runDepsFlow (기존)                       → gopedia(required dep) 먼저 설치
         ├─ hub GET /packages/.../install-script    → npm install / docker pull
         ├─ runScriptFlow (기존)                     → 바이너리/이미지 준비
         └─ NEW: runMCPRegistration(pkg, s, reader)
                 ├─ env_schema 프롬프트 (기존 등록값을 default로 재제시)
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

마이그레이션 파일: `deploy/migrations/NNNN_add_mcp_metadata.up.sql` / `.down.sql` (NNNN은 머지 시점 다음 번호).

### mcp_metadata JSON 구조

```json
{
  "server_name": "gopedia",
  "transport": "stdio",
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

**필드 명세:**
- `transport`: `"stdio"`(기본) | `"http"` | `"sse"`. v1은 `"stdio"`만 구현. http/sse는 스키마 자리만 마련 — 미구현 시 등록 단계에서 `transport not supported` 에러. 향후 데이터-only 확장 가능.
- `native.command` + `docker.image` 동시 누락 불가 (전략 어느 쪽도 못 만듦 → 의미 없음).
- `args` 안의 템플릿 토큰은 다음 namespace만 허용 (hub validation 시점에 검사):
  - `{{image}}` → `image:version` (docker 전략 한정)
  - `{{slug}}` → 패키지 slug
  - `{{version}}` → 설치되는 version
  - `{{env:NAME}}` → env_schema에 선언된 NAME 값 (등록 시점에 CLI가 치환)
  - 이외 토큰은 hub publish 단계에서 422 reject.
- `platform_overrides[GOOS]`는 base에 native/docker 키 단위로 셸로우 머지.
- `env_schema[].secret: true`면 prompt 시 입력 echo 끔 + 이후 어떤 stdout/stderr 출력에도 값 노출 금지(I8 참조).
- `env_schema[].name`은 `^[A-Z][A-Z0-9_]*$` 정규식. `server_name`은 `^[a-z][a-z0-9_-]*$`.

### Code Changes

- `internal/domain/package.go`: `Package.MCPMetadata *MCPMetadata`. JSON Marshal/Unmarshal, JSONB scan/value 구현.
- `internal/registry/store.go`: 모든 SELECT/INSERT 쿼리에 `mcp_metadata` 컬럼 추가. NULL 처리.
- `internal/api/`:
  - POST/PUT `/packages` 핸들러에서 `mcp_metadata` 수신.
  - 검증: `type=mcp`이면 `mcp_metadata` 필수, `server_name`/`native or docker 최소 하나`/`env_schema[].name` 모두 정규식 일치, args templating 토큰이 허용 namespace 내. 누락·위반 시 400/422 + 사람이 읽을 만한 에러.
  - GET `/packages/{slug}` 응답에 포함.
- `internal/installscript/llm.go`:
  - mcp 타입 패키지의 install script 생성 프롬프트 분기. 결과 스크립트는 **바이너리/이미지 준비까지만** 생성 (클라이언트 등록 명령 포함 금지).
  - **post-generation validation**: 생성된 스크립트가 다음 forbidden 패턴 포함 시 422 reject — `claude mcp`, `cursor`, `mcpServers`, `~/.claude.json`, `~/.cursor`, `~/.gemini`, `mcp.servers`. LLM 지시문에 의존하지 않고 코드로 검증.

### Tests

- `registry/store_test.go`: `mcp_metadata` round-trip (Create → GetBySlug → assert deep equal).
- `api/*_test.go`:
  - POST type=mcp with valid metadata → 201, GET 응답 검증.
  - POST type=mcp without metadata → 400.
  - POST type=mcp with invalid metadata (server_name 비음, native/docker 양쪽 비음, env name 정규식 위반, 알 수 없는 templating 토큰) → 400/422.
- `installscript/llm_test.go`: mcp 타입 시 프롬프트가 분기 변형을 포함하는지 + forbidden 패턴 reject 동작.

## CLI Changes (morphso)

### New package: `internal/mcpclient/`

```
internal/mcpclient/
├── client.go         # 인터페이스 + DetectInstalled()
├── claude_code.go    # Claude Code 구현 (~/.claude.json 또는 claude CLI subprocess)
├── cursor.go         # Cursor 구현 (~/.cursor/mcp.json, flat mcpServers)
├── gemini_cli.go     # Gemini CLI 구현 (~/.gemini/settings.json, mcp.servers 중첩)
└── *_test.go
```

**Interface:**
```go
type MCPClient interface {
    Name() string                                              // "claude-code", "cursor", "gemini-cli"
    Detected() bool                                            // 클라이언트 설치 감지
    ExistingEntry(serverName string) (*MCPServerEntry, error)  // 기존 등록 항목 조회 (없으면 nil)
    Register(serverName string, entry MCPServerEntry) error    // 등록/갱신 (atomic write)
    Unregister(serverName string) error                        // 제거 (idempotent)
}

type MCPServerEntry struct {
    Command string
    Args    []string
    Env     map[string]string
}

func DetectInstalled() []MCPClient   // 등록된 모든 클라이언트 중 Detected()=true 인 것만
```

**Detected() 정의:** "클라이언트가 시스템에 설치된 것으로 보임" 의 휴리스틱. 구현체별:
- Claude Code: `claude` 바이너리가 PATH에 존재 **OR** `~/.claude.json` 파일 존재
- Cursor: `~/.cursor/` 디렉토리 존재 (앱 첫 실행 시 생성됨)
- Gemini CLI: `gemini` 바이너리가 PATH에 존재 **OR** `~/.gemini/` 디렉토리 존재

Register 시점에 config 파일이 없으면 생성 (디렉토리 포함).

### 클라이언트별 등록 전략 (실측 기반)

**Claude Code (대상: Claude **Code** CLI, NOT Claude Desktop)**
- 등록 방식: **subprocess 우선** — `claude mcp add-json <server_name> <json> --scope user` 호출. 공식 CLI가 scope·precedence를 처리하므로 우리는 재구현하지 않음.
- subprocess 실패/`claude` 부재 시: 직접 편집 fallback — `~/.claude.json` 의 `mcpServers` 키 (user scope) 또는 `projects[<cwd>].mcpServers` (project scope). v1 기본은 **user scope** (`moso install`이 임의 cwd에서 실행되므로 안전한 디폴트).
- 파일 경로 (darwin/linux/windows 공통): `$HOME/.claude.json`. Windows는 `%USERPROFILE%\.claude.json` — 단, v1은 windows 미구현.

**Cursor**
- 파일: `~/.cursor/mcp.json` (darwin/linux 동일)
- JSON 구조: 최상위 `mcpServers` (flat). 다른 키 보존.

**Gemini CLI**
- 파일: `~/.gemini/settings.json`
- JSON 구조: **`mcp.servers`** (nested). 다른 키(특히 `ide`, `security`, `GEMINI_SYSTEM_MD` 등) 보존.

각 구현체가 자신의 JSON 경로를 캡슐화 — 인터페이스 레벨에서는 "mcpServers" 같은 가정 없음.

### 쓰기 안전성 (모든 구현체 공통 규약)

1. config 파일 읽기 → JSON unmarshal.
2. **파일은 있는데 unmarshal 실패** → 즉시 에러 반환 (`config 손상: %s — 수동으로 복구 필요`). 자동 덮어쓰기 절대 금지. (I2)
3. 파일 없음 → 빈 객체에서 시작.
4. 자기 영역(mcpServers 또는 mcp.servers)에 머지: `entry[serverName] = ...`. 다른 키·다른 server 항목 모두 보존.
5. `.bak` 파일 생성 → tmp 파일에 marshal (`encoding/json` 사용, 절대 string-template 금지) → `os.Rename(tmp, target)` (atomic).
6. 권한: 0600 유지.
7. 동시 실행: v1은 **last-writer-wins** 명시적 수용. flock 등 락은 v2 검토 (I3).

### hub client types (`internal/hub/types.go`, `parse.go`)

`Package` JSON unmarshal에 `MCPMetadata *MCPMetadata` 필드 추가. 새 타입:

```go
type MCPMetadata struct {
    ServerName        string                    `json:"server_name"`
    Transport         string                    `json:"transport,omitempty"`         // "stdio"(default) | "http" | "sse"
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
1. `pkg.MCPMetadata` nil 검증, `Transport` 가 `"stdio"`(또는 빈 값) 아니면 에러 (`transport %q not supported in v1`).
2. `mergeOverride(meta, runtime.GOOS)` — base + GOOS override 머지. `runtime.GOOS == "windows"` 면 v1은 `windows registration not implemented` 메시지 출력하고 registration step 전체 skip.
3. `mcpclient.DetectInstalled()` 한 번 호출 → 감지된 클라이언트 목록 보관.
4. 각 감지 클라이언트에서 `ExistingEntry(server_name)` 조회 → 합쳐서 `existingEnv map[string]string` 구성 (충돌 시 첫 발견 값을 default 후보로 사용). 이로써 재설치 시 사용자가 모든 값을 다시 칠 필요 없음 (I1).
5. env_schema 순회:
   - **default 우선순위**: `existingEnv[name]` > `schema.Default`.
   - `--yes`: `os.Getenv(name)` → 없으면 위 default → required면서 전부 비면 error.
   - 일반 prompt: `[기본: <default 또는 ***>]` 표시 (secret이면 `***`로 마스킹). 빈 입력 → default 사용. secret이면 stty echo off.
   - `--reconfigure` 플래그 시 `existingEnv` 무시 (강제 재입력).
6. `MCPServerEntry` 구성:
   - native: `{Command: meta.Native.Command, Args: expandTokens(meta.Native.Args, ctx), Env: collected}`.
   - docker: `{Command: "docker", Args: expandTokens(meta.Docker.Args, ctx), Env: collected}`.
   - `expandTokens`: `{{image}}`, `{{slug}}`, `{{version}}`, `{{env:NAME}}` 만 치환. 알 수 없는 토큰 → error.
   - **docker env forwarding 규약**: 시크릿은 `Env:` 맵에만 넣고, args 안의 `-e VAR` 는 값 없이 사용해 클라이언트가 spawn하는 process env를 통해 docker가 forward. args에 `{{env:VAR}}` 치환을 통해 plaintext 값을 넣는 패턴은 **금지**(ps에 노출). hub validator도 이 패턴을 막음. (C4)
7. 감지된 각 클라이언트에 `Register(server_name, entry)`. 결과 라인 출력:
   - `✓ claude-code에 'gopedia' 등록`
   - `· cursor: 미감지, skip`
   - `✗ gemini-cli: 등록 실패: ...`
   - **출력 어디에도 env 값 자체는 표시하지 않음**(secret이든 아니든) — 일관성과 secret 누출 방지를 위해. (I8)
8. 등록 결과(`registered_clients []string`)는 `RecordInstall` 호출 시 메타로 전달(향후 hub-side 옵션 필드; 현재 hub schema에는 추가하지 않고, RecordInstall payload 확장은 v2로 deferred — 일단 CLI 측 로그에만 남김).

### `cmd/remove.go`: Unregister 통합

remove 흐름에서 type=mcp인 패키지일 때:
- `pkg.MCPMetadata.ServerName` 기준으로 `mcpclient.DetectInstalled()` 각각에 `Unregister(name)`.
- **사용자가 등록 후 수동 수정한 경우에도 무조건 삭제** (idempotent). 보존이 필요한 사용자는 manual config 편집 사용. 단순성 우선. (I5)
- 등록 안 됐어도 조용히 skip.

### `internal/installer/installer.go`

현재 코드(`installer.go:46`):
```go
case "pip", "mcp", "recipe", "binary", "helm":
    fallthrough
default:
    return []string{"pip", "install", pkg}
```

**변경**: `case "mcp"` 를 fallthrough 그룹에서 분리:
```go
case "mcp":
    pkg := slug
    if version != "" { pkg += "@" + version }
    return []string{"npm", "install", "-g", pkg}
case "pip", "recipe", "binary", "helm":
    fallthrough
default:
    return []string{"pip", "install", pkg}
```

- BuildCommand는 hub install script가 없을 때만 도달하는 fallback. mcp 패키지는 정상 케이스에서 hub script 사용.
- 단, mcp + docker strategy + hub script 없는 경우: BuildCommand 호출 전에 명시적 에러 (`mcp 타입 + docker 전략은 hub install script 필요`). 이는 `cmd/install.go` 의 fallback 분기에서 처리.
- pip/recipe/binary/helm 의 pip fallback은 기존 동작 유지 (이 PR 범위 밖, 알려진 smell로 표시만).

### Recommendation (`internal/hub/deps.go` LocalRecommend)

- 입력 `pkg.Type == "mcp"`일 때:
  - `npm` 가용 + Node ≥ 18 가정 시 → `native` 추천.
  - 아니면 docker 가용 시 → `docker`.
  - 둘 다 없음 → 에러.

### 중간 실패 / Secret 처리 규약

- env 값은 **메모리에만** 보관. install 도중 실패 시 폐기. tempfile 등에 절대 저장 금지. 재실행 시 다시 입력 (또는 ExistingEntry로 자동 default). (I4)
- secret 표시 규약 (I8):
  - prompt 시 stty echo off
  - 어떤 stdout/stderr 출력에도 값 원문 금지
  - 디폴트 표시는 항상 `***`
  - log/RecordInstall 어디에도 포함 안 함

### Tests

- `mcpclient/*_test.go`: 임시 `$HOME` 픽스처, 각 클라이언트마다:
  - Detected: 바이너리/디렉토리/파일 유무 케이스.
  - Register: 각 클라이언트의 정확한 JSON 경로(`mcpServers` vs `mcp.servers`)에 쓰기, 기존 항목 보존, 동일 이름 덮어쓰기.
  - **손상된 JSON 파일** → Register 거부 (덮어쓰지 않음).
  - Unregister: 존재/미존재 idempotent. 사용자 수정된 entry도 삭제.
  - 다른 최상위 키 보존 (`theme`, `ide`, `security` 등 라운드트립).
  - atomic write: rename 실패 주입 시 원본 보존, .bak 존재 확인.
  - **Claude Code**: subprocess 경로(`claude mcp add-json` 가용 시) + 직접 편집 fallback 둘 다 테스트.
- `cmd/install.go`: runMCPRegistration 단위 테스트 (mcpclient 인터페이스 스파이로):
  - env_schema prompt 흐름 (입력 stub).
  - **ExistingEntry로부터 default 가져옴** (재설치 시).
  - `--reconfigure` → existing 무시.
  - `--yes` 모드 env/default fallback.
  - 누락 required env → 에러.
  - 토큰 치환 (`{{image}}`, `{{slug}}`, `{{version}}`, `{{env:NAME}}`).
  - 알 수 없는 토큰 → 에러.
  - GOOS override 머지.
  - windows → registration skip + 메시지.
  - transport != "stdio" → 에러.
  - secret 값이 어떤 출력에도 안 나오는지 (출력 캡처 후 검증).

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
  transport: stdio
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
- install script (hub LLM 자동 생성 후 검토 + forbidden-pattern validation 통과): hub의 install script는 (slug, version, strategy) 단위로 저장되므로 version은 저장 시점에 hub가 스크립트 본문에 박는다. CLI 측 추가 env 주입 불필요.
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
3. gopedia-mcp 패키지 데이터 publish + install script 등록 (hub validation 통과 확인).
4. E2E 검증: 깨끗한 환경에서 `moso install gopedia-mcp` → 감지된 클라이언트 config에 등록 확인 → 실제 MCP 도구 호출 (Claude Code 기준 `/mcp` 확인).

**롤백 안전성:**
- hub: 컬럼 추가만 — 기존 패키지 무영향. type≠mcp 흐름 변화 없음.
- CLI: type=mcp 분기에만 신규 동작.
- 등록 실패 시 install 자체는 성공 유지(바이너리/이미지 준비됨). 수동 등록 가이드 출력.
- 모든 client config 쓰기는 `.bak` 백업 + atomic rename + 손상 파일 자동 덮어쓰기 금지.

## Known Limitations (v1)

- **Windows**: registration step 미구현, 명시적 skip + 메시지 출력. 경로/override 스키마만 마련.
- **HTTP/SSE transport**: 스키마 자리만, v1은 stdio만 등록 가능. 외 transport는 등록 단계 에러.
- **동시 실행**: last-writer-wins. flock 등 정식 락은 v2 검토.
- **Pip fallback for pip/recipe/binary/helm 타입**: `installer.go` 의 알려진 smell — 이 PR 범위 밖.
- **RecordInstall에 registered_clients 보존 안 함**: hub schema 변경 부담을 피해 v2 deferred. CLI stdout으로만 노출.
