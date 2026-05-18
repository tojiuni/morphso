# moso install LLM-powered Script Generation Design

## Goal

`moso install gopedia --docker` 한 줄로 어떤 OS에서도 패키지를 설치할 수 있게 한다. hub가 LLM을 활용해 install script를 제공하며, 캐시 히트 시 LLM 소모 없이 즉시 반환한다. Free/Pro 티어에 따라 Claude API(빠름) 또는 Ollama(느림, 무료)로 라우팅한다.

---

## Architecture

```
moso install gopedia@1.2.3 --docker
         │
         ├─ CLI: slug=gopedia, version=1.2.3, strategy=docker
         │
         └─ GET /packages/gopedia/install-script?version=1.2.3&strategy=docker
                    │
                    ├─ [캐시 히트] install_scripts 테이블에 존재
                    │       └─ 즉시 반환 (LLM 소모 없음)
                    │
                    └─ [캐시 미스] 스크립트 없음
                               ├─ Pro 또는 오늘 < 10회 → Claude API (빠름, 고품질)
                               └─ Free + 10회 이상    → Ollama  (느림, 무료)
                               └─ 생성 → DB 캐시 저장 → 반환

CLI 수신 후:
  ├─ script preview 출력
  ├─ "Proceed? [Y/n]"  (--yes 로 스킵)
  └─ MOSO_CONFIG=./gopedia.env sh /tmp/moso-install-gopedia.sh
```

### LLM 라우팅 규칙

| 상황 | 모델 | 크레딧 소모 |
|------|------|-----------|
| 캐시 히트 | 없음 | 없음 |
| Pro 유저 | Claude API | 현재 무제한 (결제 연동 후 정의) |
| Free, 오늘 < 10회 | Claude API | +1 카운트 |
| Free, 오늘 ≥ 10회 | Ollama | 없음 (무료, 느림) |

---

## Hub 변경사항

### 새 DB 마이그레이션

**migration 005 — install_scripts:**
```sql
CREATE TABLE install_scripts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    package_slug    TEXT NOT NULL,
    version         TEXT NOT NULL DEFAULT 'latest',
    strategy        TEXT NOT NULL,
    script          TEXT NOT NULL,
    config_template TEXT NOT NULL DEFAULT '',
    sha256          TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(package_slug, version, strategy)
);
CREATE INDEX idx_install_scripts_slug ON install_scripts(package_slug);
```

**migration 006 — llm_usage:**
```sql
CREATE TABLE llm_usage (
    user_id    TEXT NOT NULL,
    date       DATE NOT NULL DEFAULT CURRENT_DATE,
    count      INT  NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, date)
);
```

### 새 API 엔드포인트

| Method | Path | Auth | 설명 |
|--------|------|------|------|
| `GET` | `/packages/{slug}/install-script` | 선택 | 스크립트 반환 (캐시 or LLM 생성) |
| `PUT` | `/packages/{slug}/install-script` | 필수 (publisher) | 수동 스크립트 등록 |
| `GET` | `/packages/{slug}/install-script/template` | 없음 | config template 반환 |

**Query params (GET install-script):**
- `version` — 기본값 `latest`
- `strategy` — `native` | `docker` | `k8s` | `helm`

**응답 (GET install-script):**
```json
{
  "script": "#!/bin/sh\ndocker pull myregistry.io/gopedia:1.2.3\ndocker run -d --name gopedia ...",
  "sha256": "sha256:abc123...",
  "version": "1.2.3",
  "has_template": true,
  "model_used": "claude"
}
```

### 새 Go 파일

**`internal/llm/service.go`** — LLM 서비스 인터페이스:
```go
type LLMService interface {
    GenerateInstallScript(pkg *domain.Package, version, strategy string) (string, error)
}
```
구현체: `ClaudeClient`, `OllamaClient`

LLM 프롬프트에 제공되는 컨텍스트: `pkg.Slug`, `pkg.Type`, `pkg.Name`, `pkg.RecommendedStrategy`, version, strategy, artifact URL (package_versions 테이블). 이 정보로 LLM이 실행 가능한 sh 스크립트를 생성한다.

**`internal/llm/router.go`** — 라우팅 결정:
```go
func (r *Router) Route(userID string, tier string) LLMService {
    if tier == "pro" {
        return r.claude
    }
    if r.dailyCount(userID) < 10 {
        return r.claude
    }
    return r.ollama
}
```

**`internal/api/handlers/install_scripts.go`** — 핸들러:
- `GetInstallScript` — 캐시 조회 → 미스 시 LLM 호출 → 캐시 저장 → 반환
- `PutInstallScript` — publisher 수동 등록 (auth 필요)
- `GetInstallTemplate` — config template 반환

---

## CLI 변경사항

### `@version` 파싱 (`cmd/install.go`)

```go
slug := args[0]
version := "latest"
if idx := strings.LastIndex(slug, "@"); idx != -1 {
    version = slug[idx+1:]
    slug = slug[:idx]
}
```

### 새 플래그

```
--template          config template을 ./gopedia.env로 저장
--config <file>     커스텀 config 파일 경로 (스크립트에 env로 주입)
```
기존 `--yes`, `--docker`, `--native`, `--k8s`, `--helm` 유지.

### 설치 실행 흐름 변경

기존 `installer.BuildCommand` 호출 대신:

```
1. client.GetInstallScript(slug, version, strategy)
   ├─ 200 OK → script 수신
   └─ 404    → 기존 BuildCommand 로컬 로직 fallback

2. sha256 검증 (불일치 시 실행 차단)

3. script preview 출력 (stdout)

4. "Proceed? [Y/n]"  (--yes 스킵)

5. --config 파일 있으면 env 주입:
   MOSO_CONFIG=./gopedia.env sh /tmp/moso-install-<slug>-<random>.sh

6. 완료 → client.RecordInstall 기록
```

### `--template` 동작

```
$ moso install gopedia --docker --template
Config template saved to ./gopedia.env
Edit it and re-run:
  moso install gopedia --docker --config ./gopedia.env
```

### 새 hub client 메서드 (`internal/hub/client.go`)

```go
type InstallScript struct {
    Script      string `json:"script"`
    SHA256      string `json:"sha256"`
    Version     string `json:"version"`
    HasTemplate bool   `json:"has_template"`
    ModelUsed   string `json:"model_used"`
}

func (c *Client) GetInstallScript(slug, version, strategy string) (*InstallScript, error)
func (c *Client) GetInstallTemplate(slug, strategy string) (string, error)
```

---

## 에러 처리

| 상황 | 동작 |
|------|------|
| hub 404 (스크립트 없음) | 로컬 `BuildCommand` fallback |
| hub 503 (LLM 장애) | "잠시 후 재시도하세요" + exit 1 |
| sha256 불일치 | "스크립트 무결성 오류" + 실행 차단 |
| sh 실행 실패 | exit code + stderr 출력 |
| Free ≥ 10회 초과 | 정상 동작, `model_used: "ollama"` 표시 |

---

## 테스트 범위

### Hub

- `install_scripts` 캐시 히트 → LLM 미호출 확인
- `install_scripts` 캐시 미스 → LLM 호출 → DB 저장 확인
- LLM 라우팅: Pro → Claude, Free < 10 → Claude, Free ≥ 10 → Ollama
- `llm_usage` count 증감 (일별 리셋)
- `PUT install-script`: 인증 없음 → 401, 인증 있음 → 200
- `GET install-script/template`: 200 반환, template 없으면 404

### CLI

- `@version` 파싱: `gopedia@1.2.3` → `(gopedia, 1.2.3)`, `gopedia` → `(gopedia, latest)`
- `GetInstallScript` mock 200 → preview 출력, sha256 검증
- `--yes` 플래그로 confirm 스킵
- hub 404 → `BuildCommand` fallback 호출 확인
- sha256 불일치 → 실행 차단
- `--template` → `./gopedia.env` 파일 생성 확인

---

## 미래 구현 예정 (이번 범위 외)

- Stripe + Toss Payments 결제 연동 (Pro 구독)
- LLM 생성 스크립트 품질 평가 및 자동 재생성
- `moso publish` 시 자동 스크립트 사전 생성
- gopedia 패키지 morphso-hub 등록 (첫 번째 패키지)
