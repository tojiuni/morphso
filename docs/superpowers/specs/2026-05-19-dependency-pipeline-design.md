# Package Dependency Pipeline Design

## Goal

morphso-hub에 패키지 의존성 데이터를 관리하는 파이프라인을 추가한다. 패키지 설치 요청 시 관련 의존 서비스도 함께 설치하고, morphso CLI에서 사전 리소스 충분성을 검증한다. 의존성 데이터는 gopedia를 지식 베이스로 사용하며, LLM은 데이터 miss 시에만 호출하고 결과는 DB에 저장해 점진적으로 확장한다.

---

## Architecture Overview

```
morphso CLI
  │
  ├─ GET /packages/{slug}/dependencies   ← Hub
  ├─ GET /users/me/installs              ← Hub
  ├─ spec.Collect()                      ← 로컬 시스템 측정
  │
  └─ 설치 플랜 구성 → 리소스 경고 → 확인 → deps 순차 설치 → main 설치

morphso-hub
  │
  ├─ package_dependencies (PostgreSQL)   구조화 캐시
  ├─ gopedia                             지식 베이스 (search + ingest)
  └─ LLM (Ollama/Claude)                 데이터 miss 시에만 호출
```

---

## DB Schema (morphso-hub PostgreSQL)

### Migration 007 — package_dependencies

```sql
CREATE TABLE package_dependencies (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  package_slug    TEXT NOT NULL REFERENCES packages(slug) ON DELETE CASCADE,
  depends_on_slug TEXT NOT NULL REFERENCES packages(slug) ON DELETE CASCADE,
  min_version     TEXT,
  source          TEXT NOT NULL DEFAULT 'author',  -- 'author' | 'llm'
  gopedia_doc_id  UUID,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (package_slug, depends_on_slug)
);

CREATE INDEX idx_deps_package ON package_dependencies(package_slug);
```

- `source='author'`: 패키지 author가 PUT API로 직접 등록
- `source='llm'`: LLM이 install script 분석으로 자동 감지
- author가 등록한 항목은 LLM이 덮어쓰지 않는다
- `gopedia_doc_id`: gopedia 문서와 연결, update/delete 시 gopedia 동기화에 사용

### Migration 008 — enhance install_history

```sql
ALTER TABLE install_history
  ADD COLUMN install_group_id   UUID,
  ADD COLUMN install_source     TEXT NOT NULL DEFAULT 'user',
  ADD COLUMN status             TEXT NOT NULL DEFAULT 'success',
  ADD COLUMN error_message      TEXT;

-- 'install_source' values: 'user' | 'dependency'
-- 'status' values: 'success' | 'failed' | 'skipped'

CREATE INDEX idx_install_group  ON install_history(install_group_id);
CREATE INDEX idx_install_source ON install_history(user_id, install_source);
```

- `install_group_id`: 하나의 `moso install` 세션에서 발생한 모든 설치(main + deps)에 같은 UUID를 부여한다. CLI가 세션 시작 시 UUID를 생성하고 모든 RecordInstall 호출에 포함한다. deps가 main보다 먼저 설치되므로 parent FK 대신 group ID로 묶는다.
- `install_source`: 사용자 직접 설치('user') vs 의존성 자동 설치('dependency') 구분

### Migration 009 — package_downloads

```sql
CREATE TABLE package_downloads (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id       TEXT,
  package_slug  TEXT NOT NULL REFERENCES packages(slug),
  version       TEXT NOT NULL,
  downloaded_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_downloads_slug ON package_downloads(package_slug);
CREATE INDEX idx_downloads_user ON package_downloads(user_id);
```

- 무료 패키지 포함 모든 install script 요청 시 기록
- `user_id = NULL`: 비인증 요청

---

## Gopedia Integration (morphso-hub)

### 문서 구조

패키지당 1개의 의존성 문서를 gopedia에 저장한다.

```markdown
# {slug} Dependencies

## Direct Dependencies

| Package    | Min Version | Role                        |
|------------|-------------|-----------------------------|
| postgresql | 15.0        | taxon 스토리지용 관계형 DB   |
| qdrant     |             | 벡터 시맨틱 검색              |
| typedb     | 2.0         | 지식 그래프 DB               |

## Resource Requirements (docker)
- RAM: 8 GB minimum
- Disk: 20 GB minimum
- GPU: false

## Metadata
- source: llm
- script_sha256: sha256:abc123
- updated_at: 2026-05-19T10:00:00Z
```

ingest 파라미터:
- `title`: `"{slug} Dependencies"`
- `source`: `"packages/{slug}/dependencies"` — slug 기반 정확 타겟팅
- `collection`: `"package-dependencies"`
- `tags`: `["dependencies", "{slug}", "morphso-package"]`

### GopediaClient (internal/gopedia/client.go)

```go
type Client struct {
    baseURL string
    http    *http.Client
}

func NewClient(baseURL string) *Client

// SearchDependencies: GET /api/search?q=packages/{slug}/dependencies&format=json
// source_path 필터로 정확 매칭
func (c *Client) SearchDependencies(ctx context.Context, slug string) (*DepDocument, error)

// IngestDependencies: POST /api/ingest_content
// 반환된 doc_id를 package_dependencies.gopedia_doc_id에 저장
func (c *Client) IngestDependencies(ctx context.Context, slug string, doc *DepDocument) (docID string, error)
```

### 조회 Flow (3단계 cache-first)

```
GET /packages/{slug}/dependencies

Step 1: PostgreSQL 조회
  → rows 존재 → 즉시 반환 (LLM, gopedia 호출 없음)

Step 2: PostgreSQL miss → gopedia 검색
  GET /api/search?q=packages/{slug}/dependencies&format=json
  → hit → 문서 파싱 → PostgreSQL에 upsert (source='llm') → 반환

Step 3: gopedia miss → LLM 분석
  → install_scripts 테이블에서 캐시된 스크립트 조회
  → 스크립트 없으면 빈 배열 반환 (스크립트 먼저 생성 필요)
  → LLM: "이 스크립트가 의존하는 외부 서비스 목록을 JSON으로"
  → 결과를 morphso 등록 패키지와 매칭
  → PostgreSQL에 저장 (source='llm')
  → gopedia ingest (지식 베이스 확장)
  → 반환
```

author가 `PUT /packages/{slug}/dependencies`로 등록하면:
- PostgreSQL에 source='author'로 upsert
- gopedia 문서 업데이트 (같은 source_path로 재ingest)

---

## Hub API

### 의존성 관리

**GET /packages/{slug}/dependencies** (공개, optional auth)

Response:
```json
{
  "dependencies": [
    {
      "package": {
        "slug": "postgresql",
        "name": "PostgreSQL",
        "type": "docker"
      },
      "min_version": "15.0",
      "source": "author",
      "resource_requirements": {
        "min_memory_gb": 1,
        "min_disk_gb": 5,
        "needs_gpu": false
      }
    }
  ]
}
```

**PUT /packages/{slug}/dependencies** (required auth, author only)

Request:
```json
{
  "dependencies": [
    { "slug": "postgresql", "min_version": "15.0" },
    { "slug": "qdrant" },
    { "slug": "typedb", "min_version": "2.0" }
  ]
}
```

- author의 기존 deps는 교체, LLM deps는 유지
- PUT 후 gopedia 문서 업데이트

### 이력 조회

**GET /users/me/installs** (기존, 확장)

Response에 `install_source`, `status`, `parent_install_id` 필드 추가.

**GET /users/me/purchases** (신규, required auth)

Response:
```json
{
  "purchases": [
    {
      "package_slug": "gopedia",
      "version": "1.0.0",
      "amount_cents": 0,
      "purchased_at": "2026-05-19T10:00:00Z"
    }
  ]
}
```

**GET /packages/{slug}/stats** (공개)

Response:
```json
{
  "slug": "gopedia",
  "total_downloads": 1240,
  "total_installs": 890,
  "unique_users": 340
}
```

---

## CLI Flow

### 의존성 포함 설치 (moso install gopedia --docker)

```
1. GET /packages/gopedia               패키지 정보
2. GET /packages/gopedia/dependencies  의존성 목록
3. GET /users/me/installs              설치 이력 (인증 시)
4. spec.Collect()                      로컬 리소스 측정

5. Plan 구성:
   각 dep에 대해:
   - 설치 이력에 있고 버전 >= min_version → skip
   - 설치 이력에 있고 버전 < min_version  → update
   - 설치 이력 없음                        → install
   리소스 합산: install + update 대상만 합산

6. 플랜 출력:
   Installing gopedia@latest [docker]

   Dependencies:
     ✓ postgresql  v15.2  (설치됨, skip)
     ↑ qdrant             (v1.1 → v1.7, 업데이트)
     + typedb      v2.0   (신규 설치)

   Required:  RAM 8GB  Disk 20GB
   Available: RAM 6GB  Disk 50GB
   ⚠ RAM 2GB 부족합니다. 계속 진행할까요? [y/N]

7. 확인 (--yes로 skip)
   → deps 순차 설치 (qdrant update → typedb install)
   → main 설치 (gopedia)

8. 각 설치 후 RecordInstall (모두 같은 group_id 공유):
   group_id = uuid.New()  (세션 시작 시 CLI가 생성)
   typedb:  { install_source='dependency', group_id=group_id }
   qdrant:  { install_source='dependency', group_id=group_id }
   gopedia: { install_source='user',       group_id=group_id }

   POST /installs 응답: { "id": "uuid" }  (생성된 레코드 ID 반환)
```

### 신규 플래그

| 플래그 | 설명 |
|--------|------|
| `--no-deps` | 의존성 설치 없이 main만 설치 |
| `--yes` | 기존: 확인 프롬프트 skip (리소스 경고도 skip) |

### hub.Client 신규 메서드 (morphso)

```go
// internal/hub/types.go 추가
type DependencyInfo struct {
    Package              Package
    MinVersion           string
    Source               string
    ResourceRequirements ResourceRequirements
}

type DependencyResponse struct {
    Dependencies []DependencyInfo
}

type ResourceRequirements struct {
    MinMemoryGB float64 `json:"min_memory_gb"`
    MinDiskGB   float64 `json:"min_disk_gb"`
    NeedsGPU    bool    `json:"needs_gpu"`
}

// InstallRecord 기존 타입에 필드 추가
type InstallRecord struct {
    // ... 기존 필드 ...
    InstallGroupID string `json:"install_group_id,omitempty"`
    InstallSource  string `json:"install_source,omitempty"`
    Status         string `json:"status,omitempty"`
}

// POST /installs 응답 타입 추가
type InstallRecordResponse struct {
    ID string `json:"id"`
}

// internal/hub/client.go 추가
func (c *Client) GetDependencies(slug string) (*DependencyResponse, error)
// RecordInstall 시그니처 변경: InstallRequest에 GroupID, Source 필드 추가
// 반환값: (installID string, error)
```

---

## Transitive Dependencies

1단계(직접 의존성)만 처리한다. typedb가 또 다른 패키지에 의존하더라도 CLI는 재귀 조회하지 않는다. 추후 B-level 재귀 해결로 확장 가능하도록 DependencyInfo 구조체는 `depth int` 필드를 예약한다.

---

## Configuration (morphso-hub)

```
GOPEDIA_URL      # default: http://localhost:8787
```

`config.go`에 `GopediaURL` 필드 추가. gopedia client는 의존성 store에 주입한다.

---

## Testing Strategy

**morphso-hub:**
- `DependencyStore` interface — PostgreSQL 구현체 단위 테스트
- `GopediaClient` — httptest.Server mock으로 search/ingest 응답 테스트
- `DependencyHandler` — 3단계 flow 각각 (PostgreSQL hit / gopedia hit / LLM 생성) 테스트

**morphso CLI:**
- `BuildDependencyPlan()` — install/update/skip 판정 로직 단위 테스트
- `CheckResources()` — 리소스 부족 감지 단위 테스트
- `runInstall` — `--no-deps` 플래그 동작, deps 순차 실행 통합 테스트

---

## File Map

**morphso-hub (신규/변경):**

| 파일 | 역할 |
|------|------|
| `internal/db/migrations/007_package_dependencies.up.sql` | 의존성 테이블 |
| `internal/db/migrations/008_enhance_install_history.up.sql` | install_history 컬럼 추가 |
| `internal/db/migrations/009_package_downloads.up.sql` | 다운로드 이력 테이블 |
| `internal/dependency/types.go` | Dependency 도메인 타입 |
| `internal/dependency/store.go` | DependencyStore interface + PG 구현 |
| `internal/dependency/llm.go` | LLM 의존성 감지 (buildDepPrompt, parseDepsJSON) |
| `internal/gopedia/client.go` | GopediaClient (search, ingest) |
| `internal/api/handlers/dependencies.go` | GET/PUT /dependencies 핸들러 |
| `internal/api/handlers/history.go` | GET /users/me/purchases, /packages/{slug}/stats |
| `internal/api/router.go` | 라우트 등록 |
| `internal/config/config.go` | GOPEDIA_URL 추가 |
| `cmd/server/main.go` | DependencyHandler, GopediaClient 와이어업 |

**morphso CLI (신규/변경):**

| 파일 | 역할 |
|------|------|
| `internal/hub/types.go` | DependencyInfo, DependencyResponse, ResourceRequirements 추가 |
| `internal/hub/client.go` | GetDependencies, RecordInstallWithParent 추가 |
| `internal/hub/deps.go` | BuildDependencyPlan, CheckResources |
| `internal/hub/deps_test.go` | Plan 로직 단위 테스트 |
| `cmd/install.go` | deps fetch → 리소스 체크 → 순차 설치 flow 추가, --no-deps 플래그 |
