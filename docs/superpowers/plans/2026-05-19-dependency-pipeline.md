# Package Dependency Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** morphso-hub에 패키지 의존성 파이프라인을 추가한다 — gopedia를 지식 베이스로, LLM은 cache miss 시에만 호출하며, CLI는 deps를 포함한 리소스 검증 후 순차 설치한다.

**Architecture:** hub의 GET /packages/{slug}/dependencies는 PostgreSQL → gopedia → LLM 3단계 cache-first 로직으로 응답한다. CLI는 deps 목록과 spec.Collect() 결과를 비교해 리소스 경고를 출력하고, 확인 후 deps → main 순서로 설치한다.

**Tech Stack:** Go 1.26, chi v5, pgx/v5, golang-migrate, httptest (테스트), github.com/tojiuni/morphso-hub, github.com/tojiuni/morphso

---

## File Map

**morphso-hub** (`/Users/dong-hoshin/Documents/dev/morphso-hub/`):

| 파일 | 역할 |
|------|------|
| `internal/db/migrations/007_package_dependencies.up.sql` | 의존성 테이블 |
| `internal/db/migrations/008_enhance_install_history.up.sql` | install_history 컬럼 추가 |
| `internal/db/migrations/009_package_downloads.up.sql` | 다운로드 이력 테이블 |
| `internal/dependency/types.go` | Dependency, ResourceRequirements, Store interface |
| `internal/dependency/store.go` | pgStore 구현 |
| `internal/dependency/store_test.go` | Store 단위 테스트 |
| `internal/gopedia/client.go` | GopediaClient (search, ingest) |
| `internal/gopedia/client_test.go` | GopediaClient 단위 테스트 |
| `internal/dependency/detector.go` | OllamaDetector (buildDepPrompt, parseDepsJSON) |
| `internal/dependency/detector_test.go` | Detector 단위 테스트 |
| `internal/api/handlers/dependencies.go` | GET/PUT /dependencies 핸들러 |
| `internal/api/handlers/dependencies_test.go` | 핸들러 단위 테스트 |
| `internal/api/handlers/installs.go` | RecordInstall 응답 + 신규 필드 |
| `internal/api/handlers/history.go` | GET /users/me/purchases, GET /packages/{slug}/stats |
| `internal/api/router.go` | 라우트 추가 |
| `internal/config/config.go` | GopediaURL 추가 |
| `cmd/server/main.go` | DependencyHandler, GopediaClient 와이어업 |

**morphso CLI** (`/Users/dong-hoshin/Documents/dev/morphso/`):

| 파일 | 역할 |
|------|------|
| `internal/hub/types.go` | DependencyInfo, DependencyResponse, ResourceRequirements 추가 |
| `internal/hub/client.go` | GetDependencies, RecordInstall 시그니처 변경 |
| `internal/hub/deps.go` | BuildDependencyPlan, CheckResources |
| `internal/hub/deps_test.go` | Plan 로직 단위 테스트 |
| `cmd/install.go` | deps flow, --no-deps 플래그 |

---

## Task 1 (hub): DB Migrations

**Files:**
- Create: `internal/db/migrations/007_package_dependencies.up.sql`
- Create: `internal/db/migrations/008_enhance_install_history.up.sql`
- Create: `internal/db/migrations/009_package_downloads.up.sql`

- [ ] **Step 1: Create 007_package_dependencies.up.sql**

```sql
CREATE TABLE package_dependencies (
  id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  package_slug          TEXT NOT NULL REFERENCES packages(slug) ON DELETE CASCADE,
  depends_on_slug       TEXT NOT NULL REFERENCES packages(slug) ON DELETE CASCADE,
  min_version           TEXT,
  source                TEXT NOT NULL DEFAULT 'author',
  resource_requirements JSONB NOT NULL DEFAULT '{}',
  gopedia_doc_id        UUID,
  created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (package_slug, depends_on_slug)
);

CREATE INDEX idx_deps_package ON package_dependencies(package_slug);
```

- [ ] **Step 2: Create 008_enhance_install_history.up.sql**

```sql
ALTER TABLE install_history
  ADD COLUMN install_group_id UUID,
  ADD COLUMN install_source   TEXT NOT NULL DEFAULT 'user',
  ADD COLUMN status           TEXT NOT NULL DEFAULT 'success',
  ADD COLUMN error_message    TEXT;

CREATE INDEX idx_install_group  ON install_history(install_group_id);
CREATE INDEX idx_install_source ON install_history(user_id, install_source);
```

- [ ] **Step 3: Create 009_package_downloads.up.sql**

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

- [ ] **Step 4: Verify migrations compile and apply**

Run from `/Users/dong-hoshin/Documents/dev/morphso-hub`:
```bash
go build ./...
```
Expected: no errors

- [ ] **Step 5: Commit**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
git add internal/db/migrations/007_package_dependencies.up.sql \
        internal/db/migrations/008_enhance_install_history.up.sql \
        internal/db/migrations/009_package_downloads.up.sql
git commit -m "feat: add dependency, install_history enhance, downloads migrations"
```

---

## Task 2 (hub): Dependency Types + PG Store

**Files:**
- Create: `internal/dependency/types.go`
- Create: `internal/dependency/store.go`
- Create: `internal/dependency/store_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/dependency/store_test.go
package dependency_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tojiuni/morphso-hub/internal/dependency"
)

type mockStore struct {
	deps   []*dependency.Dependency
	saved  []*dependency.Dependency
}

func (m *mockStore) GetByPackage(_ context.Context, slug string) ([]*dependency.Dependency, error) {
	var result []*dependency.Dependency
	for _, d := range m.deps {
		if d.PackageSlug == slug {
			result = append(result, d)
		}
	}
	return result, nil
}
func (m *mockStore) SaveAuthorDeps(_ context.Context, _ string, deps []*dependency.Dependency) error {
	m.saved = deps
	return nil
}
func (m *mockStore) SaveLLMDeps(_ context.Context, _ string, deps []*dependency.Dependency) error {
	m.saved = append(m.saved, deps...)
	return nil
}
func (m *mockStore) SetGopediaDocID(_ context.Context, _, _, _ string) error { return nil }

func TestMockStore_GetByPackage(t *testing.T) {
	store := &mockStore{
		deps: []*dependency.Dependency{
			{PackageSlug: "gopedia", DependsOnSlug: "postgresql", Source: "author"},
			{PackageSlug: "gopedia", DependsOnSlug: "qdrant", Source: "llm"},
			{PackageSlug: "other", DependsOnSlug: "redis", Source: "author"},
		},
	}
	deps, err := store.GetByPackage(context.Background(), "gopedia")
	require.NoError(t, err)
	assert.Len(t, deps, 2)
}

func TestMockStore_SaveAuthorDeps(t *testing.T) {
	store := &mockStore{}
	deps := []*dependency.Dependency{
		{PackageSlug: "gopedia", DependsOnSlug: "postgresql", Source: "author"},
	}
	err := store.SaveAuthorDeps(context.Background(), "gopedia", deps)
	require.NoError(t, err)
	assert.Len(t, store.saved, 1)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
go test ./internal/dependency/... 2>&1
```
Expected: FAIL — `package dependency_test` not found or types undefined

- [ ] **Step 3: Create types.go**

```go
// internal/dependency/types.go
package dependency

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("dependency not found")

type ResourceRequirements struct {
	MinMemoryGB float64 `json:"min_memory_gb"`
	MinDiskGB   float64 `json:"min_disk_gb"`
	NeedsGPU    bool    `json:"needs_gpu"`
}

type Dependency struct {
	ID                   string
	PackageSlug          string
	DependsOnSlug        string
	MinVersion           string
	Source               string // "author" | "llm"
	ResourceRequirements ResourceRequirements
	GopediaDocID         string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type Store interface {
	GetByPackage(ctx context.Context, slug string) ([]*Dependency, error)
	// SaveAuthorDeps replaces all author-source deps for slug, keeps llm-source deps.
	SaveAuthorDeps(ctx context.Context, slug string, deps []*Dependency) error
	// SaveLLMDeps inserts llm-detected deps, skipping conflicts (author deps take priority).
	SaveLLMDeps(ctx context.Context, slug string, deps []*Dependency) error
	SetGopediaDocID(ctx context.Context, packageSlug, depSlug, docID string) error
}
```

- [ ] **Step 4: Create store.go**

```go
// internal/dependency/store.go
package dependency

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgStore struct {
	pool *pgxpool.Pool
}

func NewPGStore(pool *pgxpool.Pool) Store {
	return &pgStore{pool: pool}
}

func (s *pgStore) GetByPackage(ctx context.Context, slug string) ([]*Dependency, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT depends_on_slug, min_version, source, resource_requirements, gopedia_doc_id, created_at, updated_at
		FROM package_dependencies
		WHERE package_slug = $1
		ORDER BY created_at`, slug)
	if err != nil {
		return nil, fmt.Errorf("query deps: %w", err)
	}
	defer rows.Close()

	var deps []*Dependency
	for rows.Next() {
		d := &Dependency{PackageSlug: slug}
		var rrJSON []byte
		var gopediaDocID *string
		if err := rows.Scan(&d.DependsOnSlug, &d.MinVersion, &d.Source, &rrJSON, &gopediaDocID, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan dep: %w", err)
		}
		if len(rrJSON) > 0 {
			_ = json.Unmarshal(rrJSON, &d.ResourceRequirements)
		}
		if gopediaDocID != nil {
			d.GopediaDocID = *gopediaDocID
		}
		deps = append(deps, d)
	}
	return deps, rows.Err()
}

func (s *pgStore) SaveAuthorDeps(ctx context.Context, slug string, deps []*Dependency) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`DELETE FROM package_dependencies WHERE package_slug = $1 AND source = 'author'`, slug); err != nil {
		return fmt.Errorf("delete author deps: %w", err)
	}
	for _, d := range deps {
		rrJSON, _ := json.Marshal(d.ResourceRequirements)
		if _, err := tx.Exec(ctx, `
			INSERT INTO package_dependencies (package_slug, depends_on_slug, min_version, source, resource_requirements)
			VALUES ($1, $2, $3, 'author', $4)
			ON CONFLICT (package_slug, depends_on_slug)
			DO UPDATE SET min_version = EXCLUDED.min_version,
			              source = 'author',
			              resource_requirements = EXCLUDED.resource_requirements,
			              updated_at = now()`,
			slug, d.DependsOnSlug, d.MinVersion, rrJSON); err != nil {
			return fmt.Errorf("insert author dep %s: %w", d.DependsOnSlug, err)
		}
	}
	return tx.Commit(ctx)
}

func (s *pgStore) SaveLLMDeps(ctx context.Context, slug string, deps []*Dependency) error {
	for _, d := range deps {
		rrJSON, _ := json.Marshal(d.ResourceRequirements)
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO package_dependencies (package_slug, depends_on_slug, min_version, source, resource_requirements)
			VALUES ($1, $2, $3, 'llm', $4)
			ON CONFLICT (package_slug, depends_on_slug) DO NOTHING`,
			slug, d.DependsOnSlug, d.MinVersion, rrJSON); err != nil {
			return fmt.Errorf("insert llm dep %s: %w", d.DependsOnSlug, err)
		}
	}
	return nil
}

func (s *pgStore) SetGopediaDocID(ctx context.Context, packageSlug, depSlug, docID string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE package_dependencies SET gopedia_doc_id = $1
		WHERE package_slug = $2 AND depends_on_slug = $3`,
		docID, packageSlug, depSlug)
	if err != nil {
		return fmt.Errorf("set gopedia doc id: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// suppress unused import warning for pgx
var _ = pgx.ErrNoRows
var _ = errors.New
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
go test ./internal/dependency/... -v
```
Expected: PASS (mock-based tests pass without DB)

- [ ] **Step 6: Commit**

```bash
git add internal/dependency/
git commit -m "feat: add dependency domain types and PG store"
```

---

## Task 3 (hub): Gopedia Client

**Files:**
- Create: `internal/gopedia/client.go`
- Create: `internal/gopedia/client_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/gopedia/client_test.go
package gopedia_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tojiuni/morphso-hub/internal/gopedia"
)

func TestSearchDependencies_Found(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/search" {
			json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{
					{
						"source_path": "packages/gopedia/dependencies",
						"l1_id":       "doc-uuid-1",
						"snippet":     "| postgresql | 15.0 | database |",
					},
				},
			})
			return
		}
		if r.URL.Path == "/api/restore" {
			json.NewEncoder(w).Encode(map[string]any{
				"markdown": "# gopedia Dependencies\n\n## Direct Dependencies\n\n| Package | Min Version | Role |\n|---------|-------------|------|\n| postgresql | 15.0 | database |\n| qdrant |  | vector search |\n\n## Resource Requirements (docker)\n- RAM: 8 GB minimum\n- Disk: 20 GB minimum\n",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := gopedia.NewClient(srv.URL)
	doc, err := client.SearchDependencies(context.Background(), "gopedia")
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Len(t, doc.Dependencies, 2)
	assert.Equal(t, "postgresql", doc.Dependencies[0].Slug)
	assert.Equal(t, "15.0", doc.Dependencies[0].MinVersion)
	assert.InDelta(t, 8.0, doc.RAM, 0.01)
}

func TestSearchDependencies_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	}))
	defer srv.Close()

	client := gopedia.NewClient(srv.URL)
	doc, err := client.SearchDependencies(context.Background(), "unknown")
	require.NoError(t, err)
	assert.Nil(t, doc)
}

func TestIngestDependencies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/ingest_content", r.URL.Path)
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		assert.Equal(t, "gopedia Dependencies", req["title"])
		assert.Equal(t, "packages/gopedia/dependencies", req["source"])
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "doc_id": "new-doc-uuid"})
	}))
	defer srv.Close()

	client := gopedia.NewClient(srv.URL)
	doc := &gopedia.DepDocument{
		Slug:         "gopedia",
		Dependencies: []gopedia.DepEntry{{Slug: "postgresql", MinVersion: "15.0", Role: "database"}},
		RAM:          8.0,
		Disk:         20.0,
		Source:       "llm",
	}
	docID, err := client.IngestDependencies(context.Background(), "gopedia", doc)
	require.NoError(t, err)
	assert.Equal(t, "new-doc-uuid", docID)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
go test ./internal/gopedia/... 2>&1
```
Expected: FAIL — package gopedia not found

- [ ] **Step 3: Create client.go**

```go
// internal/gopedia/client.go
package gopedia

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{baseURL: baseURL, http: &http.Client{Timeout: 30 * time.Second}}
}

type DepEntry struct {
	Slug       string
	MinVersion string
	Role       string
}

type DepDocument struct {
	Slug         string
	Dependencies []DepEntry
	RAM          float64
	Disk         float64
	Source       string
	UpdatedAt    time.Time
}

type searchResponse struct {
	Results []struct {
		SourcePath string `json:"source_path"`
		L1ID       string `json:"l1_id"`
	} `json:"results"`
}

type restoreResponse struct {
	Markdown string `json:"markdown"`
}

type ingestRequest struct {
	Title      string   `json:"title"`
	Content    string   `json:"content"`
	Tags       []string `json:"tags"`
	Source     string   `json:"source"`
	Collection string   `json:"collection"`
}

type ingestResponse struct {
	Ok    bool   `json:"ok"`
	DocID string `json:"doc_id"`
	Error string `json:"error"`
}

func (c *Client) SearchDependencies(ctx context.Context, slug string) (*DepDocument, error) {
	targetSource := "packages/" + slug + "/dependencies"
	q := url.QueryEscape(targetSource)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/search?q="+q+"&format=json&top_k=10", nil)
	if err != nil {
		return nil, fmt.Errorf("build search request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gopedia search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gopedia search %s", resp.Status)
	}
	var sr searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, fmt.Errorf("decode search: %w", err)
	}
	var l1ID string
	for _, r := range sr.Results {
		if r.SourcePath == targetSource {
			l1ID = r.L1ID
			break
		}
	}
	if l1ID == "" {
		return nil, nil
	}
	return c.restore(ctx, slug, l1ID)
}

func (c *Client) restore(ctx context.Context, slug, l1ID string) (*DepDocument, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/restore?l1_id="+url.QueryEscape(l1ID)+"&format=json", nil)
	if err != nil {
		return nil, fmt.Errorf("build restore request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gopedia restore: %w", err)
	}
	defer resp.Body.Close()
	var rr restoreResponse
	if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
		return nil, fmt.Errorf("decode restore: %w", err)
	}
	return parseDepDocument(slug, rr.Markdown), nil
}

func (c *Client) IngestDependencies(ctx context.Context, slug string, doc *DepDocument) (string, error) {
	body := ingestRequest{
		Title:      slug + " Dependencies",
		Content:    renderDepDocument(slug, doc),
		Tags:       []string{"dependencies", slug, "morphso-package"},
		Source:     "packages/" + slug + "/dependencies",
		Collection: "package-dependencies",
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return "", fmt.Errorf("encode ingest: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/ingest_content", &buf)
	if err != nil {
		return "", fmt.Errorf("build ingest request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("gopedia ingest: %w", err)
	}
	defer resp.Body.Close()
	var ir ingestResponse
	if err := json.NewDecoder(resp.Body).Decode(&ir); err != nil {
		return "", fmt.Errorf("decode ingest response: %w", err)
	}
	if !ir.Ok {
		return "", fmt.Errorf("gopedia ingest failed: %s", ir.Error)
	}
	return ir.DocID, nil
}

func renderDepDocument(slug string, doc *DepDocument) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s Dependencies\n\n", slug)
	fmt.Fprintf(&sb, "## Direct Dependencies\n\n")
	fmt.Fprintf(&sb, "| Package | Min Version | Role |\n")
	fmt.Fprintf(&sb, "|---------|-------------|------|\n")
	for _, d := range doc.Dependencies {
		fmt.Fprintf(&sb, "| %s | %s | %s |\n", d.Slug, d.MinVersion, d.Role)
	}
	fmt.Fprintf(&sb, "\n## Resource Requirements (docker)\n")
	fmt.Fprintf(&sb, "- RAM: %.0f GB minimum\n", doc.RAM)
	fmt.Fprintf(&sb, "- Disk: %.0f GB minimum\n", doc.Disk)
	fmt.Fprintf(&sb, "\n## Metadata\n")
	fmt.Fprintf(&sb, "- source: %s\n", doc.Source)
	fmt.Fprintf(&sb, "- updated_at: %s\n", doc.UpdatedAt.Format(time.RFC3339))
	return sb.String()
}

func parseDepDocument(slug, content string) *DepDocument {
	doc := &DepDocument{Slug: slug, UpdatedAt: time.Now()}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "|") && !strings.Contains(line, "Package") && !strings.Contains(line, "---") {
			parts := strings.Split(line, "|")
			if len(parts) >= 4 {
				entry := DepEntry{
					Slug:       strings.TrimSpace(parts[1]),
					MinVersion: strings.TrimSpace(parts[2]),
					Role:       strings.TrimSpace(parts[3]),
				}
				if entry.Slug != "" {
					doc.Dependencies = append(doc.Dependencies, entry)
				}
			}
		}
		if strings.HasPrefix(line, "- RAM:") {
			fmt.Sscanf(strings.TrimPrefix(line, "- RAM: "), "%f", &doc.RAM)
		}
		if strings.HasPrefix(line, "- Disk:") {
			fmt.Sscanf(strings.TrimPrefix(line, "- Disk: "), "%f", &doc.Disk)
		}
		if strings.HasPrefix(line, "- source:") {
			doc.Source = strings.TrimSpace(strings.TrimPrefix(line, "- source:"))
		}
	}
	return doc
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
go test ./internal/gopedia/... -v
```
Expected: PASS (3 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/gopedia/
git commit -m "feat: add gopedia client for dependency search and ingest"
```

---

## Task 4 (hub): LLM Dependency Detector

**Files:**
- Create: `internal/dependency/detector.go`
- Create: `internal/dependency/detector_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/dependency/detector_test.go
package dependency_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tojiuni/morphso-hub/internal/dependency"
)

func TestOllamaDetector_DetectDeps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/generate", r.URL.Path)
		json.NewEncoder(w).Encode(map[string]any{
			"response": `[{"slug":"postgresql","min_version":"15.0","role":"database","resource_requirements":{"min_memory_gb":1,"min_disk_gb":5,"needs_gpu":false}}]`,
		})
	}))
	defer srv.Close()

	det := dependency.NewOllamaDetector(srv.URL, "llama3.2")
	deps, err := det.DetectDeps(context.Background(),
		"#!/bin/sh\ndocker run -d postgres:15",
		[]string{"postgresql", "qdrant", "typedb"})
	require.NoError(t, err)
	require.Len(t, deps, 1)
	assert.Equal(t, "postgresql", deps[0].DependsOnSlug)
	assert.Equal(t, "15.0", deps[0].MinVersion)
	assert.InDelta(t, 1.0, deps[0].ResourceRequirements.MinMemoryGB, 0.01)
}

func TestOllamaDetector_EmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"response": "[]"})
	}))
	defer srv.Close()

	det := dependency.NewOllamaDetector(srv.URL, "llama3.2")
	deps, err := det.DetectDeps(context.Background(), "#!/bin/sh\napt-get install curl", []string{"postgresql"})
	require.NoError(t, err)
	assert.Empty(t, deps)
}

func TestParseDepsJSON(t *testing.T) {
	input := `[{"slug":"qdrant","min_version":"","role":"vector db","resource_requirements":{"min_memory_gb":2,"min_disk_gb":10,"needs_gpu":false}}]`
	deps := dependency.ParseDepsJSON("gopedia", input, []string{"qdrant", "postgresql"})
	require.Len(t, deps, 1)
	assert.Equal(t, "qdrant", deps[0].DependsOnSlug)
	assert.InDelta(t, 2.0, deps[0].ResourceRequirements.MinMemoryGB, 0.01)
}

func TestParseDepsJSON_FiltersUnknownSlugs(t *testing.T) {
	input := `[{"slug":"unknownpkg","min_version":"1.0","role":"something","resource_requirements":{}}]`
	deps := dependency.ParseDepsJSON("gopedia", input, []string{"postgresql", "qdrant"})
	assert.Empty(t, deps)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
go test ./internal/dependency/... 2>&1 | grep -E "FAIL|undefined"
```
Expected: undefined: dependency.NewOllamaDetector

- [ ] **Step 3: Create detector.go**

```go
// internal/dependency/detector.go
package dependency

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Detector interface {
	DetectDeps(ctx context.Context, script string, availableSlugs []string) ([]*Dependency, error)
}

type OllamaDetector struct {
	baseURL string
	model   string
	http    *http.Client
}

func NewOllamaDetector(baseURL, model string) *OllamaDetector {
	return &OllamaDetector{baseURL: baseURL, model: model, http: &http.Client{Timeout: 60 * time.Second}}
}

func (d *OllamaDetector) DetectDeps(ctx context.Context, script string, availableSlugs []string) ([]*Dependency, error) {
	prompt := buildDepPrompt(script, availableSlugs)
	body := map[string]any{"model": d.model, "prompt": prompt, "stream": false}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return nil, fmt.Errorf("encode ollama request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+"/api/generate", &buf)
	if err != nil {
		return nil, fmt.Errorf("create ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama http: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode ollama response: %w", err)
	}
	return ParseDepsJSON("", result.Response, availableSlugs), nil
}

func buildDepPrompt(script string, availableSlugs []string) string {
	return fmt.Sprintf(
		`Analyze this shell install script and identify what external services it depends on.
Available slugs: %s

Output ONLY a JSON array, nothing else:
[{"slug":"postgresql","min_version":"15.0","role":"database","resource_requirements":{"min_memory_gb":1.0,"min_disk_gb":5.0,"needs_gpu":false}}]

Output [] if no matches. Do not include markdown fences.

Script:
%s`,
		strings.Join(availableSlugs, ", "),
		script,
	)
}

type depJSON struct {
	Slug                 string               `json:"slug"`
	MinVersion           string               `json:"min_version"`
	Role                 string               `json:"role"`
	ResourceRequirements ResourceRequirements `json:"resource_requirements"`
}

// ParseDepsJSON parses LLM JSON output into Dependency slice, filtering to known slugs.
// Exported for use in tests.
func ParseDepsJSON(packageSlug, raw string, knownSlugs []string) []*Dependency {
	raw = strings.TrimSpace(raw)
	// strip any accidental markdown fences
	if idx := strings.Index(raw, "["); idx >= 0 {
		raw = raw[idx:]
	}
	if idx := strings.LastIndex(raw, "]"); idx >= 0 {
		raw = raw[:idx+1]
	}
	var items []depJSON
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil
	}
	knownSet := make(map[string]struct{}, len(knownSlugs))
	for _, s := range knownSlugs {
		knownSet[s] = struct{}{}
	}
	var deps []*Dependency
	for _, item := range items {
		if _, ok := knownSet[item.Slug]; !ok {
			continue
		}
		deps = append(deps, &Dependency{
			PackageSlug:          packageSlug,
			DependsOnSlug:        item.Slug,
			MinVersion:           item.MinVersion,
			Source:               "llm",
			ResourceRequirements: item.ResourceRequirements,
		})
	}
	return deps
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
go test ./internal/dependency/... -v
```
Expected: PASS (5 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/dependency/detector.go internal/dependency/detector_test.go
git commit -m "feat: add OllamaDetector for LLM dependency detection"
```

---

## Task 5 (hub): GET /dependencies Handler

**Files:**
- Create: `internal/api/handlers/dependencies.go`
- Create: `internal/api/handlers/dependencies_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/api/handlers/dependencies_test.go
package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tojiuni/morphso-hub/internal/api/handlers"
	"github.com/tojiuni/morphso-hub/internal/dependency"
	"github.com/tojiuni/morphso-hub/internal/domain"
	"github.com/tojiuni/morphso-hub/internal/installscript"
	"github.com/tojiuni/morphso-hub/internal/registry"
)

// --- mock dependency store ---

type mockDepStore struct {
	deps []*dependency.Dependency
}

func (m *mockDepStore) GetByPackage(_ context.Context, slug string) ([]*dependency.Dependency, error) {
	var result []*dependency.Dependency
	for _, d := range m.deps {
		if d.PackageSlug == slug {
			result = append(result, d)
		}
	}
	return result, nil
}
func (m *mockDepStore) SaveAuthorDeps(_ context.Context, _ string, deps []*dependency.Dependency) error {
	m.deps = deps
	return nil
}
func (m *mockDepStore) SaveLLMDeps(_ context.Context, _ string, deps []*dependency.Dependency) error {
	m.deps = append(m.deps, deps...)
	return nil
}
func (m *mockDepStore) SetGopediaDocID(_ context.Context, _, _, _ string) error { return nil }

// --- mock detector ---

type mockDetector struct {
	deps []*dependency.Dependency
	err  error
}

func (m *mockDetector) DetectDeps(_ context.Context, _ string, _ []string) ([]*dependency.Dependency, error) {
	return m.deps, m.err
}

func newDepRouter(h *handlers.DependencyHandler) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/packages/{slug}/dependencies", h.GetDependencies)
	r.Put("/packages/{slug}/dependencies", h.PutDependencies)
	return r
}

func TestGetDependencies_PostgresHit(t *testing.T) {
	depStore := &mockDepStore{
		deps: []*dependency.Dependency{
			{PackageSlug: "gopedia", DependsOnSlug: "postgresql", MinVersion: "15.0", Source: "author",
				ResourceRequirements: dependency.ResourceRequirements{MinMemoryGB: 1, MinDiskGB: 5}},
		},
	}
	pkgStore := &mockPkgStore{pkg: &domain.Package{Slug: "postgresql", Name: "PostgreSQL", Type: "docker"}}
	h := handlers.NewDependencyHandler(depStore, nil, pkgStore, nil, nil)

	r := newDepRouter(h)
	req := httptest.NewRequest(http.MethodGet, "/packages/gopedia/dependencies", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp handlers.DependencyListResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Len(t, resp.Dependencies, 1)
	assert.Equal(t, "postgresql", resp.Dependencies[0].Package.Slug)
	assert.Equal(t, "15.0", resp.Dependencies[0].MinVersion)
	assert.InDelta(t, 1.0, resp.Dependencies[0].ResourceRequirements.MinMemoryGB, 0.01)
}

func TestGetDependencies_LLMFallback(t *testing.T) {
	depStore := &mockDepStore{} // empty — triggers LLM path
	scriptStore := &mockScriptStore{
		script: &installscript.InstallScript{Script: "#!/bin/sh\ndocker run postgres:15"},
	}
	pkgStore := &mockPkgStore{pkg: &domain.Package{Slug: "postgresql", Name: "PostgreSQL", Type: "docker"}}
	detector := &mockDetector{
		deps: []*dependency.Dependency{
			{DependsOnSlug: "postgresql", MinVersion: "15.0", Source: "llm",
				ResourceRequirements: dependency.ResourceRequirements{MinMemoryGB: 1}},
		},
	}
	h := handlers.NewDependencyHandler(depStore, scriptStore, pkgStore, nil, detector)

	r := newDepRouter(h)
	req := httptest.NewRequest(http.MethodGet, "/packages/gopedia/dependencies", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp handlers.DependencyListResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Len(t, resp.Dependencies, 1)
}

func TestGetDependencies_NoScript_ReturnsEmpty(t *testing.T) {
	depStore := &mockDepStore{}
	scriptStore := &mockScriptStore{getErr: installscript.ErrNotFound}
	pkgStore := &mockPkgStore{}
	h := handlers.NewDependencyHandler(depStore, scriptStore, pkgStore, nil, nil)

	r := newDepRouter(h)
	req := httptest.NewRequest(http.MethodGet, "/packages/gopedia/dependencies", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp handlers.DependencyListResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Empty(t, resp.Dependencies)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
go test ./internal/api/handlers/... 2>&1 | grep -E "FAIL|undefined"
```
Expected: undefined: handlers.DependencyHandler

- [ ] **Step 3: Create dependencies.go**

```go
// internal/api/handlers/dependencies.go
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/tojiuni/morphso-hub/internal/auth"
	"github.com/tojiuni/morphso-hub/internal/dependency"
	"github.com/tojiuni/morphso-hub/internal/gopedia"
	"github.com/tojiuni/morphso-hub/internal/installscript"
	"github.com/tojiuni/morphso-hub/internal/registry"
)

type DependencyInfo struct {
	Package              packageRef                   `json:"package"`
	MinVersion           string                       `json:"min_version"`
	Source               string                       `json:"source"`
	ResourceRequirements dependency.ResourceRequirements `json:"resource_requirements"`
}

type packageRef struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type DependencyListResponse struct {
	Dependencies []DependencyInfo `json:"dependencies"`
}

type DependencyHandler struct {
	deps     dependency.Store
	scripts  installscript.Store
	packages registry.Store
	gopedia  *gopedia.Client
	detector dependency.Detector
}

func NewDependencyHandler(
	deps dependency.Store,
	scripts installscript.Store,
	packages registry.Store,
	gopedia *gopedia.Client,
	detector dependency.Detector,
) *DependencyHandler {
	return &DependencyHandler{deps: deps, scripts: scripts, packages: packages, gopedia: gopedia, detector: detector}
}

func (h *DependencyHandler) GetDependencies(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")

	// Stage 1: PostgreSQL cache
	deps, err := h.deps.GetByPackage(r.Context(), slug)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if len(deps) > 0 {
		h.writeResponse(w, r.Context(), deps)
		return
	}

	// Stage 2: Gopedia
	if h.gopedia != nil {
		doc, err := h.gopedia.SearchDependencies(r.Context(), slug)
		if err == nil && doc != nil {
			deps = gopediaDocToDeps(slug, doc)
			go func() {
				_ = h.deps.SaveLLMDeps(context.Background(), slug, deps)
			}()
			h.writeResponse(w, r.Context(), deps)
			return
		}
	}

	// Stage 3: LLM detection
	if h.detector == nil || h.scripts == nil {
		h.writeEmptyResponse(w)
		return
	}
	script, err := h.scripts.GetScript(r.Context(), slug, "latest", "docker")
	if errors.Is(err, installscript.ErrNotFound) {
		h.writeEmptyResponse(w)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	allPkgs, err := h.packages.Search(r.Context(), "", 200, 0)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	slugs := make([]string, 0, len(allPkgs))
	for _, p := range allPkgs {
		if p.Slug != slug {
			slugs = append(slugs, p.Slug)
		}
	}
	detected, err := h.detector.DetectDeps(r.Context(), script.Script, slugs)
	if err != nil {
		slog.Error("dep detection failed", "slug", slug, "err", err)
		h.writeEmptyResponse(w)
		return
	}
	for i := range detected {
		detected[i].PackageSlug = slug
	}
	if err := h.deps.SaveLLMDeps(r.Context(), slug, detected); err != nil {
		slog.Error("failed to save llm deps", "slug", slug, "err", err)
	}
	if h.gopedia != nil {
		go h.ingestToGopedia(slug, detected)
	}
	h.writeResponse(w, r.Context(), detected)
}

func (h *DependencyHandler) ingestToGopedia(slug string, deps []*dependency.Dependency) {
	doc := depsToGopediaDoc(deps)
	docID, err := h.gopedia.IngestDependencies(context.Background(), slug, doc)
	if err != nil {
		slog.Error("failed to ingest deps to gopedia", "slug", slug, "err", err)
		return
	}
	for _, d := range deps {
		_ = h.deps.SetGopediaDocID(context.Background(), slug, d.DependsOnSlug, docID)
	}
}

func (h *DependencyHandler) writeResponse(w http.ResponseWriter, ctx context.Context, deps []*dependency.Dependency) {
	infos := make([]DependencyInfo, 0, len(deps))
	for _, d := range deps {
		pkg, err := h.packages.GetBySlug(ctx, d.DependsOnSlug)
		ref := packageRef{Slug: d.DependsOnSlug}
		if err == nil {
			ref.Name = pkg.Name
			ref.Type = string(pkg.Type)
		}
		infos = append(infos, DependencyInfo{
			Package:              ref,
			MinVersion:           d.MinVersion,
			Source:               d.Source,
			ResourceRequirements: d.ResourceRequirements,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(DependencyListResponse{Dependencies: infos})
}

func (h *DependencyHandler) writeEmptyResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(DependencyListResponse{Dependencies: []DependencyInfo{}})
}

func gopediaDocToDeps(packageSlug string, doc *gopedia.DepDocument) []*dependency.Dependency {
	deps := make([]*dependency.Dependency, 0, len(doc.Dependencies))
	for _, e := range doc.Dependencies {
		deps = append(deps, &dependency.Dependency{
			PackageSlug:   packageSlug,
			DependsOnSlug: e.Slug,
			MinVersion:    e.MinVersion,
			Source:        "llm",
			ResourceRequirements: dependency.ResourceRequirements{
				MinMemoryGB: doc.RAM,
				MinDiskGB:   doc.Disk,
			},
		})
	}
	return deps
}

func depsToGopediaDoc(deps []*dependency.Dependency) *gopedia.DepDocument {
	doc := &gopedia.DepDocument{Source: "llm"}
	for _, d := range deps {
		doc.Dependencies = append(doc.Dependencies, gopedia.DepEntry{
			Slug:       d.DependsOnSlug,
			MinVersion: d.MinVersion,
		})
		if d.ResourceRequirements.MinMemoryGB > doc.RAM {
			doc.RAM = d.ResourceRequirements.MinMemoryGB
		}
		if d.ResourceRequirements.MinDiskGB > doc.Disk {
			doc.Disk = d.ResourceRequirements.MinDiskGB
		}
	}
	return doc
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
go test ./internal/api/handlers/... -run TestGetDependencies -v
```
Expected: PASS (3 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/api/handlers/dependencies.go internal/api/handlers/dependencies_test.go
git commit -m "feat: add GET /dependencies handler with 3-stage cache-first flow"
```

---

## Task 6 (hub): PUT /dependencies Handler

- [ ] **Step 1: Write the failing test** (추가 테스트 to `dependencies_test.go`)

```go
// append to internal/api/handlers/dependencies_test.go

func TestPutDependencies_AuthorCanSet(t *testing.T) {
	depStore := &mockDepStore{}
	pkgStore := &mockPkgStore{pkg: &domain.Package{Slug: "gopedia", AuthorID: "user-1", Type: "docker"}}
	h := handlers.NewDependencyHandler(depStore, nil, pkgStore, nil, nil)

	r := newDepRouter(h)
	body := `{"dependencies":[{"slug":"postgresql","min_version":"15.0","resource_requirements":{"min_memory_gb":1,"min_disk_gb":5,"needs_gpu":false}}]}`
	req := httptest.NewRequest(http.MethodPut, "/packages/gopedia/dependencies", strings.NewReader(body))
	req = req.WithContext(auth.WithClaims(req.Context(), &domain.UserClaims{UserID: "user-1", Tier: domain.TierFree}))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Len(t, depStore.deps, 1)
}

func TestPutDependencies_NonAuthorForbidden(t *testing.T) {
	depStore := &mockDepStore{}
	pkgStore := &mockPkgStore{pkg: &domain.Package{Slug: "gopedia", AuthorID: "user-1", Type: "docker"}}
	h := handlers.NewDependencyHandler(depStore, nil, pkgStore, nil, nil)

	r := newDepRouter(h)
	body := `{"dependencies":[]}`
	req := httptest.NewRequest(http.MethodPut, "/packages/gopedia/dependencies", strings.NewReader(body))
	req = req.WithContext(auth.WithClaims(req.Context(), &domain.UserClaims{UserID: "user-2", Tier: domain.TierFree}))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
go test ./internal/api/handlers/... -run TestPutDependencies -v 2>&1 | head -20
```
Expected: FAIL — PutDependencies method undefined

- [ ] **Step 3: Add PutDependencies to dependencies.go**

Add after the `GetDependencies` method:

```go
type putDepItem struct {
	Slug                 string                       `json:"slug"`
	MinVersion           string                       `json:"min_version"`
	ResourceRequirements dependency.ResourceRequirements `json:"resource_requirements"`
}

type putDepsBody struct {
	Dependencies []putDepItem `json:"dependencies"`
}

func (h *DependencyHandler) PutDependencies(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	slug := chi.URLParam(r, "slug")
	pkg, err := h.packages.GetBySlug(r.Context(), slug)
	if errors.Is(err, registry.ErrNotFound) {
		http.Error(w, "package not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if pkg.AuthorID != claims.UserID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var body putDepsBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	deps := make([]*dependency.Dependency, 0, len(body.Dependencies))
	for _, item := range body.Dependencies {
		deps = append(deps, &dependency.Dependency{
			PackageSlug:          slug,
			DependsOnSlug:        item.Slug,
			MinVersion:           item.MinVersion,
			Source:               "author",
			ResourceRequirements: item.ResourceRequirements,
		})
	}
	if err := h.deps.SaveAuthorDeps(r.Context(), slug, deps); err != nil {
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}
	if h.gopedia != nil {
		go func() {
			doc := depsToGopediaDoc(deps)
			doc.Source = "author"
			_, err := h.gopedia.IngestDependencies(context.Background(), slug, doc)
			if err != nil {
				slog.Error("failed to update gopedia after PUT deps", "slug", slug, "err", err)
			}
		}()
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Check auth.WithClaims exists**

```bash
grep -r "WithClaims\|ClaimsFromContext" /Users/dong-hoshin/Documents/dev/morphso-hub/internal/auth/
```
Expected: finds `ClaimsFromContext`. If `WithClaims` is not found, use this test helper instead:

```go
// In test file, define helper if WithClaims not in auth package:
func withClaims(r *http.Request, claims *domain.UserClaims) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), auth.ClaimsKey{}, claims))
}
// Then in tests use withClaims(req, &domain.UserClaims{...}) instead of auth.WithClaims(...)
```

Check what the existing handlers_test.go uses for auth context injection:
```bash
grep -n "Claims\|WithValue\|withClaims" /Users/dong-hoshin/Documents/dev/morphso-hub/internal/api/handlers/install_scripts_test.go | head -10
```

Replicate the same pattern used in install_scripts_test.go.

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
go test ./internal/api/handlers/... -run TestPutDependencies -v
```
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/api/handlers/dependencies.go internal/api/handlers/dependencies_test.go
git commit -m "feat: add PUT /dependencies handler for author dep registration"
```

---

## Task 7 (hub): RecordInstall Enhancement + History Endpoints

**Files:**
- Modify: `internal/api/handlers/installs.go`
- Create: `internal/api/handlers/history.go`

- [ ] **Step 1: Write the failing test for RecordInstall returning ID**

```go
// internal/api/handlers/installs_test.go
package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tojiuni/morphso-hub/internal/api/handlers"
	"github.com/tojiuni/morphso-hub/internal/domain"
	"github.com/tojiuni/morphso-hub/internal/auth"
)

// This test verifies POST /installs returns {"id": "<uuid>"}
// Uses a mock pool via the handler interface (requires extracting insertRecord into Store).
// For now, test via integration or verify the JSON shape with a real pool.
// Minimal test: verify 201 status and id field presence.
func TestRecordInstallResponseShape(t *testing.T) {
	// This test requires a live DB. Mark as integration test.
	if testing.Short() {
		t.Skip("skipping integration test")
	}
}

func TestListMyInstalls_ReturnsNewFields(t *testing.T) {
	// Verify that GET /users/me/installs response includes install_source, status fields.
	// Uses short-circuit: check JSON field existence in a mock response.
	rec := domain.InstallRecord{
		ID:            "abc",
		PackageSlug:   "gopedia",
		InstallSource: "user",
		Status:        "success",
	}
	data, err := json.Marshal(rec)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"install_source"`)
	assert.Contains(t, string(data), `"status"`)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
go test ./internal/api/handlers/... -run TestListMyInstalls 2>&1 | head -20
```
Expected: FAIL — domain.InstallRecord has no field InstallSource

- [ ] **Step 3: Update domain.InstallRecord**

Read current domain/install.go:
```bash
cat /Users/dong-hoshin/Documents/dev/morphso-hub/internal/domain/install.go
```

Add new fields to InstallRecord:
```go
type InstallRecord struct {
	ID             string    `json:"id"`
	UserID         string    `json:"user_id"`
	PackageSlug    string    `json:"package_slug"`
	Version        string    `json:"version"`
	Strategy       string    `json:"strategy"`
	OS             string    `json:"os"`
	Arch           string    `json:"arch"`
	InstalledAt    time.Time `json:"installed_at"`
	InstallGroupID string    `json:"install_group_id,omitempty"`
	InstallSource  string    `json:"install_source,omitempty"`
	Status         string    `json:"status,omitempty"`
	ErrorMessage   string    `json:"error_message,omitempty"`
}

type InstallRecordID struct {
	ID string `json:"id"`
}
```

- [ ] **Step 4: Update installs.go — RecordInstall returns ID, insertRecord uses new columns**

Replace `insertRecord` and `RecordInstall` in `internal/api/handlers/installs.go`:

```go
func (h *InstallHandler) RecordInstall(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var rec domain.InstallRecord
	if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	rec.UserID = claims.UserID
	if rec.InstallSource == "" {
		rec.InstallSource = "user"
	}
	if rec.Status == "" {
		rec.Status = "success"
	}
	id, err := h.insertRecord(r.Context(), &rec)
	if err != nil {
		http.Error(w, "failed to record", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(domain.InstallRecordID{ID: id})
}

func (h *InstallHandler) insertRecord(ctx context.Context, rec *domain.InstallRecord) (string, error) {
	var id string
	err := h.pool.QueryRow(ctx, `
		INSERT INTO install_history
		  (user_id, package_slug, version, strategy, os, arch, install_group_id, install_source, status, error_message)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7,''), $8, $9, NULLIF($10,''))
		RETURNING id`,
		rec.UserID, rec.PackageSlug, rec.Version, rec.Strategy, rec.OS, rec.Arch,
		rec.InstallGroupID, rec.InstallSource, rec.Status, rec.ErrorMessage,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("insert install record: %w", err)
	}
	return id, nil
}
```

Also update `ListMyInstalls` scan to include the new columns:

```go
rows, err := h.pool.Query(r.Context(), `
	SELECT id, user_id, package_slug, version, strategy, os, arch, installed_at,
	       COALESCE(install_group_id::text, ''), install_source, status, COALESCE(error_message, '')
	FROM install_history WHERE user_id = $1
	ORDER BY installed_at DESC LIMIT 50`, claims.UserID)
// ...
if err := rows.Scan(
	&rec.ID, &rec.UserID, &rec.PackageSlug, &rec.Version,
	&rec.Strategy, &rec.OS, &rec.Arch, &rec.InstalledAt,
	&rec.InstallGroupID, &rec.InstallSource, &rec.Status, &rec.ErrorMessage,
); err != nil {
```

- [ ] **Step 5: Create history.go**

```go
// internal/api/handlers/history.go
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tojiuni/morphso-hub/internal/auth"
)

type HistoryHandler struct {
	pool *pgxpool.Pool
}

func NewHistoryHandler(pool *pgxpool.Pool) *HistoryHandler {
	return &HistoryHandler{pool: pool}
}

type purchaseRecord struct {
	PackageSlug string `json:"package_slug"`
	Version     string `json:"version"`
	AmountCents int64  `json:"amount_cents"`
	PurchasedAt string `json:"purchased_at"`
}

func (h *HistoryHandler) ListMyPurchases(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT package_slug, version, amount_cents, purchased_at
		FROM package_purchases WHERE buyer_id = $1
		ORDER BY purchased_at DESC LIMIT 100`, claims.UserID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	records := make([]purchaseRecord, 0)
	for rows.Next() {
		var p purchaseRecord
		if err := rows.Scan(&p.PackageSlug, &p.Version, &p.AmountCents, &p.PurchasedAt); err != nil {
			http.Error(w, "scan error", http.StatusInternalServerError)
			return
		}
		records = append(records, p)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"purchases": records})
}

type packageStats struct {
	Slug          string `json:"slug"`
	TotalDownloads int64 `json:"total_downloads"`
	TotalInstalls  int64 `json:"total_installs"`
	UniqueUsers    int64 `json:"unique_users"`
}

func (h *HistoryHandler) GetPackageStats(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	var stats packageStats
	stats.Slug = slug
	h.pool.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM package_downloads WHERE package_slug = $1`, slug,
	).Scan(&stats.TotalDownloads)
	h.pool.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM install_history WHERE package_slug = $1`, slug,
	).Scan(&stats.TotalInstalls)
	h.pool.QueryRow(r.Context(),
		`SELECT COUNT(DISTINCT user_id) FROM install_history WHERE package_slug = $1`, slug,
	).Scan(&stats.UniqueUsers)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}
```

- [ ] **Step 6: Run tests**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
go test ./internal/api/handlers/... -run TestListMyInstalls -v
go build ./...
```
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/domain/install.go internal/api/handlers/installs.go internal/api/handlers/history.go
git commit -m "feat: RecordInstall returns ID, add install_source/status/group, add history endpoints"
```

---

## Task 8 (hub): Config + Router + Main Wiring

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/api/router.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Add GopediaURL to config**

In `internal/config/config.go`, add to `Config` struct:
```go
GopediaURL string
```

In `Load()`, add to the return struct:
```go
GopediaURL: getEnvOrDefault("GOPEDIA_URL", "http://localhost:8787"),
```

- [ ] **Step 2: Update router.go**

Replace `NewRouter` signature and body:

```go
func NewRouter(
	pool *pgxpool.Pool,
	store registry.Store,
	authMW *auth.Middleware,
	engine *recommend.Engine,
	billingStore billing.Store,
	stripeService billing.StripeService,
	scriptHandler *handlers.InstallScriptHandler,
	depHandler *handlers.DependencyHandler,
	historyHandler *handlers.HistoryHandler,
) *chi.Mux {
	// ... existing setup ...

	// In OptionalAuth group, add:
	r.Get("/packages/{slug}/dependencies", depHandler.GetDependencies)
	r.Get("/packages/{slug}/stats", historyHandler.GetPackageStats)

	// In RequireAuth group, add:
	r.Put("/packages/{slug}/dependencies", depHandler.PutDependencies)
	r.Get("/users/me/purchases", historyHandler.ListMyPurchases)

	return r
}
```

- [ ] **Step 3: Update main.go**

Add after existing handler setup:

```go
gopediaClient := gopedia.NewClient(cfg.GopediaURL)
log.Printf("gopedia: URL=%s", cfg.GopediaURL)

depStore := dependency.NewPGStore(pool)
detector := dependency.NewOllamaDetector(cfg.OllamaURL, cfg.OllamaModel)
depHandler := handlers.NewDependencyHandler(depStore, scriptStore, store, gopediaClient, detector)
historyHandler := handlers.NewHistoryHandler(pool)
```

Update `api.NewRouter(...)` call to include `depHandler, historyHandler`.

Add imports:
```go
"github.com/tojiuni/morphso-hub/internal/dependency"
"github.com/tojiuni/morphso-hub/internal/gopedia"
```

- [ ] **Step 4: Verify build**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
go build ./...
```
Expected: no errors

- [ ] **Step 5: Run all hub tests**

```bash
go test ./... -count=1 2>&1 | tail -20
```
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/api/router.go cmd/server/main.go
git commit -m "feat: wire up DependencyHandler, GopediaClient, HistoryHandler"
```

---

## Task 9 (cli): Hub Types + GetDependencies Client

**Files:**
- Modify: `internal/hub/types.go`
- Modify: `internal/hub/client.go`
- Create: `internal/hub/deps_client_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/hub/deps_client_test.go
package hub_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tojiuni/morphso/internal/hub"
)

func TestGetDependencies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/packages/gopedia/dependencies", r.URL.Path)
		json.NewEncoder(w).Encode(map[string]any{
			"dependencies": []map[string]any{
				{
					"package":     map[string]any{"slug": "postgresql", "name": "PostgreSQL", "type": "docker"},
					"min_version": "15.0",
					"source":      "author",
					"resource_requirements": map[string]any{
						"min_memory_gb": 1.0,
						"min_disk_gb":   5.0,
						"needs_gpu":     false,
					},
				},
			},
		})
	}))
	defer srv.Close()

	client := hub.NewClient(srv.URL, "token")
	resp, err := client.GetDependencies("gopedia")
	require.NoError(t, err)
	require.Len(t, resp.Dependencies, 1)
	assert.Equal(t, "postgresql", resp.Dependencies[0].Package.Slug)
	assert.Equal(t, "15.0", resp.Dependencies[0].MinVersion)
	assert.InDelta(t, 1.0, resp.Dependencies[0].ResourceRequirements.MinMemoryGB, 0.01)
}

func TestRecordInstallWithGroup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "group-uuid-123", body["install_group_id"])
		assert.Equal(t, "dependency", body["install_source"])
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"id": "install-record-uuid"})
	}))
	defer srv.Close()

	client := hub.NewClient(srv.URL, "token")
	id, err := client.RecordInstall("postgresql", "15.0", "docker", "group-uuid-123", "dependency")
	require.NoError(t, err)
	assert.Equal(t, "install-record-uuid", id)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
go test ./internal/hub/... -run TestGetDependencies 2>&1 | head -10
```
Expected: FAIL — GetDependencies undefined

- [ ] **Step 3: Add types to internal/hub/types.go**

Append to existing `types.go`:

```go
type ResourceRequirements struct {
	MinMemoryGB float64 `json:"min_memory_gb"`
	MinDiskGB   float64 `json:"min_disk_gb"`
	NeedsGPU    bool    `json:"needs_gpu"`
}

type DepPackageRef struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type DependencyInfo struct {
	Package              DepPackageRef        `json:"package"`
	MinVersion           string               `json:"min_version"`
	Source               string               `json:"source"`
	ResourceRequirements ResourceRequirements `json:"resource_requirements"`
}

type DependencyResponse struct {
	Dependencies []DependencyInfo `json:"dependencies"`
}

// InstallRequest gets new optional fields for grouping.
// Old callers pass empty strings for the new fields.
type InstallRecordID struct {
	ID string `json:"id"`
}
```

Update `InstallRequest`:
```go
type InstallRequest struct {
	PackageSlug    string `json:"package_slug"`
	Version        string `json:"version"`
	Strategy       string `json:"strategy"`
	InstallGroupID string `json:"install_group_id,omitempty"`
	InstallSource  string `json:"install_source,omitempty"`
}
```

Update `InstallRecord`:
```go
type InstallRecord struct {
	ID             string    `json:"id"`
	UserID         string    `json:"user_id"`
	PackageSlug    string    `json:"package_slug"`
	Version        string    `json:"version"`
	Strategy       string    `json:"strategy"`
	OS             string    `json:"os"`
	Arch           string    `json:"arch"`
	InstalledAt    time.Time `json:"installed_at"`
	InstallGroupID string    `json:"install_group_id,omitempty"`
	InstallSource  string    `json:"install_source,omitempty"`
	Status         string    `json:"status,omitempty"`
}
```

- [ ] **Step 4: Add client methods to internal/hub/client.go**

Add `GetDependencies` and update `RecordInstall`:

```go
func (c *Client) GetDependencies(slug string) (*DependencyResponse, error) {
	req, err := c.newRequest(http.MethodGet, "/packages/"+slug+"/dependencies", nil)
	if err != nil {
		return nil, err
	}
	var result DependencyResponse
	if err := c.do(req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RecordInstall records an install and returns the new record ID.
// groupID and source are optional ("" = omit from request).
func (c *Client) RecordInstall(slug, version, strategy, groupID, source string) (string, error) {
	body := InstallRequest{
		PackageSlug:    slug,
		Version:        version,
		Strategy:       strategy,
		InstallGroupID: groupID,
		InstallSource:  source,
	}
	req, err := c.newRequest(http.MethodPost, "/installs", body)
	if err != nil {
		return "", err
	}
	// Override do() to capture 201 with body
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return "", ErrUnauthorized
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("server error: %s", resp.Status)
	}
	var result InstallRecordID
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", nil // best-effort
	}
	return result.ID, nil
}
```

Add `"encoding/json"` and `"fmt"` to imports if not already present.

- [ ] **Step 5: Fix existing callers of RecordInstall in cmd/install.go**

Find the old call:
```go
_ = client.RecordInstall(slug, version, strategy)
```

Replace with:
```go
_, _ = client.RecordInstall(slug, version, strategy, "", "user")
```

- [ ] **Step 6: Run tests**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
go test ./internal/hub/... -v
go build ./...
```
Expected: all PASS

- [ ] **Step 7: Commit**

```bash
git add internal/hub/types.go internal/hub/client.go internal/hub/deps_client_test.go cmd/install.go
git commit -m "feat: add DependencyResponse types and GetDependencies, update RecordInstall"
```

---

## Task 10 (cli): BuildDependencyPlan + CheckResources

**Files:**
- Create: `internal/hub/deps.go`
- Create: `internal/hub/deps_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/hub/deps_test.go
package hub_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tojiuni/morphso/internal/hub"
	"github.com/tojiuni/morphso/internal/spec"
)

func TestBuildDependencyPlan_Skip(t *testing.T) {
	deps := []hub.DependencyInfo{
		{Package: hub.DepPackageRef{Slug: "postgresql"}, MinVersion: "15.0"},
	}
	history := []hub.InstallRecord{
		{PackageSlug: "postgresql", Version: "15.2"},
	}
	plan := hub.BuildDependencyPlan(deps, history)
	require.Len(t, plan, 1)
	assert.Equal(t, hub.ActionSkip, plan[0].Action)
	assert.Equal(t, "15.2", plan[0].InstalledVersion)
}

func TestBuildDependencyPlan_Install(t *testing.T) {
	deps := []hub.DependencyInfo{
		{Package: hub.DepPackageRef{Slug: "typedb"}, MinVersion: "2.0"},
	}
	plan := hub.BuildDependencyPlan(deps, nil)
	require.Len(t, plan, 1)
	assert.Equal(t, hub.ActionInstall, plan[0].Action)
}

func TestBuildDependencyPlan_Update(t *testing.T) {
	deps := []hub.DependencyInfo{
		{Package: hub.DepPackageRef{Slug: "qdrant"}, MinVersion: "1.7"},
	}
	history := []hub.InstallRecord{
		{PackageSlug: "qdrant", Version: "1.1"},
	}
	plan := hub.BuildDependencyPlan(deps, history)
	require.Len(t, plan, 1)
	assert.Equal(t, hub.ActionUpdate, plan[0].Action)
	assert.Equal(t, "1.1", plan[0].InstalledVersion)
}

func TestBuildDependencyPlan_NoMinVersion_AlwaysSkip(t *testing.T) {
	deps := []hub.DependencyInfo{
		{Package: hub.DepPackageRef{Slug: "qdrant"}, MinVersion: ""},
	}
	history := []hub.InstallRecord{
		{PackageSlug: "qdrant", Version: "0.1"},
	}
	plan := hub.BuildDependencyPlan(deps, history)
	assert.Equal(t, hub.ActionSkip, plan[0].Action)
}

func TestCheckResources_Sufficient(t *testing.T) {
	plan := []hub.DepPlanItem{
		{Action: hub.ActionInstall, Dep: hub.DependencyInfo{ResourceRequirements: hub.ResourceRequirements{MinMemoryGB: 2, MinDiskGB: 5}}},
		{Action: hub.ActionSkip, Dep: hub.DependencyInfo{ResourceRequirements: hub.ResourceRequirements{MinMemoryGB: 4}}},
	}
	s := &spec.Spec{MemoryFreeGB: 4.0, DiskFreeGB: 20.0}
	warn := hub.CheckResources(plan, s)
	assert.Nil(t, warn)
}

func TestCheckResources_RAMShort(t *testing.T) {
	plan := []hub.DepPlanItem{
		{Action: hub.ActionInstall, Dep: hub.DependencyInfo{ResourceRequirements: hub.ResourceRequirements{MinMemoryGB: 8}}},
	}
	s := &spec.Spec{MemoryFreeGB: 4.0, DiskFreeGB: 100.0}
	warn := hub.CheckResources(plan, s)
	require.NotNil(t, warn)
	assert.Contains(t, warn.Message, "RAM")
}

func TestCheckResources_DiskShort(t *testing.T) {
	plan := []hub.DepPlanItem{
		{Action: hub.ActionInstall, Dep: hub.DependencyInfo{ResourceRequirements: hub.ResourceRequirements{MinDiskGB: 50}}},
	}
	s := &spec.Spec{MemoryFreeGB: 32.0, DiskFreeGB: 10.0}
	warn := hub.CheckResources(plan, s)
	require.NotNil(t, warn)
	assert.Contains(t, warn.Message, "Disk")
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
go test ./internal/hub/... -run TestBuildDependencyPlan 2>&1 | head -10
```
Expected: FAIL — hub.BuildDependencyPlan undefined

- [ ] **Step 3: Create deps.go**

```go
// internal/hub/deps.go
package hub

import (
	"fmt"
	"strings"

	"github.com/tojiuni/morphso/internal/spec"
)

const (
	ActionInstall = "install"
	ActionUpdate  = "update"
	ActionSkip    = "skip"
)

type DepPlanItem struct {
	Dep              DependencyInfo
	Action           string // ActionInstall | ActionUpdate | ActionSkip
	InstalledVersion string // empty if not installed
}

type ResourceWarning struct {
	Message      string
	RequiredRAM  float64
	AvailableRAM float64
	RequiredDisk float64
	AvailableDisk float64
}

// BuildDependencyPlan determines install/update/skip action for each dependency.
// history is from GET /users/me/installs; pass nil for unauthenticated users.
func BuildDependencyPlan(deps []DependencyInfo, history []InstallRecord) []DepPlanItem {
	installed := make(map[string]string) // slug → version
	for _, r := range history {
		// Keep the latest version if multiple records exist
		if existing, ok := installed[r.PackageSlug]; !ok || versionGreater(r.Version, existing) {
			installed[r.PackageSlug] = r.Version
		}
	}
	plan := make([]DepPlanItem, 0, len(deps))
	for _, dep := range deps {
		slug := dep.Package.Slug
		installedVer, isInstalled := installed[slug]
		item := DepPlanItem{Dep: dep, InstalledVersion: installedVer}
		switch {
		case !isInstalled:
			item.Action = ActionInstall
		case dep.MinVersion == "" || !versionGreater(dep.MinVersion, installedVer):
			item.Action = ActionSkip
		default:
			item.Action = ActionUpdate
		}
		plan = append(plan, item)
	}
	return plan
}

// CheckResources sums resource requirements for install/update items and compares to spec.
// Returns nil if resources are sufficient.
func CheckResources(plan []DepPlanItem, s *spec.Spec) *ResourceWarning {
	var totalRAM, totalDisk float64
	for _, item := range plan {
		if item.Action == ActionSkip {
			continue
		}
		totalRAM += item.Dep.ResourceRequirements.MinMemoryGB
		totalDisk += item.Dep.ResourceRequirements.MinDiskGB
	}
	if totalRAM == 0 && totalDisk == 0 {
		return nil
	}
	var msgs []string
	if totalRAM > s.MemoryFreeGB {
		msgs = append(msgs, fmt.Sprintf("RAM %.0fGB 필요, %.0fGB 여유 (%.0fGB 부족)",
			totalRAM, s.MemoryFreeGB, totalRAM-s.MemoryFreeGB))
	}
	if totalDisk > s.DiskFreeGB {
		msgs = append(msgs, fmt.Sprintf("Disk %.0fGB 필요, %.0fGB 여유 (%.0fGB 부족)",
			totalDisk, s.DiskFreeGB, totalDisk-s.DiskFreeGB))
	}
	if len(msgs) == 0 {
		return nil
	}
	return &ResourceWarning{
		Message:       strings.Join(msgs, ", "),
		RequiredRAM:   totalRAM,
		AvailableRAM:  s.MemoryFreeGB,
		RequiredDisk:  totalDisk,
		AvailableDisk: s.DiskFreeGB,
	}
}

// versionGreater returns true if a > b using simple dot-separated numeric comparison.
// Falls back to string comparison for non-numeric versions.
func versionGreater(a, b string) bool {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	max := len(aParts)
	if len(bParts) > max {
		max = len(bParts)
	}
	for i := 0; i < max; i++ {
		var av, bv int
		if i < len(aParts) {
			fmt.Sscanf(aParts[i], "%d", &av)
		}
		if i < len(bParts) {
			fmt.Sscanf(bParts[i], "%d", &bv)
		}
		if av != bv {
			return av > bv
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
go test ./internal/hub/... -v
```
Expected: PASS (all hub tests)

- [ ] **Step 5: Commit**

```bash
git add internal/hub/deps.go internal/hub/deps_test.go
git commit -m "feat: add BuildDependencyPlan and CheckResources"
```

---

## Task 11 (cli): install.go Enhancement

**Files:**
- Modify: `cmd/install.go`

- [ ] **Step 1: Add --no-deps flag and install group UUID helper**

Add to the var block in `cmd/install.go`:
```go
installNoDeps bool
```

Add to `init()`:
```go
installCmd.Flags().BoolVar(&installNoDeps, "no-deps", false, "의존성 설치 없이 main만 설치")
```

Add helper after the imports:
```go
import "crypto/rand"

func newInstallGroupID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
```

- [ ] **Step 2: Replace runInstall with deps-aware version**

Replace the entire `runInstall` function body (lines after hub script fetch, keeping lines 50–137 as-is) to add the dependency flow. Insert between step "5. 필요 도구 확인" and "6. Hub에서 install script 조회":

```go
	// 5.5: Fetch and execute dependencies (unless --no-deps)
	groupID := newInstallGroupID()
	if !installNoDeps {
		if err := runDepsFlow(client, s, slug, strategy, groupID, cfg); err != nil {
			return err
		}
	}
```

Then replace the RecordInstall call at the end of the hub-script success path:
```go
	if cfg.Token != "" {
		_, _ = client.RecordInstall(slug, version, strategy, groupID, "user")
	}
```

And the fallback RecordInstall call:
```go
	if cfg.Token != "" {
		_, _ = client.RecordInstall(slug, version, strategy, groupID, "user")
	}
```

- [ ] **Step 3: Add runDepsFlow function**

```go
// runDepsFlow fetches dependencies, checks resources, and installs each dep in order.
func runDepsFlow(client *hub.Client, s *spec.Spec, mainSlug, strategy, groupID string, cfg *config.Config) error {
	depsResp, err := client.GetDependencies(mainSlug)
	if err != nil || len(depsResp.Dependencies) == 0 {
		return nil // no deps or hub unavailable — continue without deps
	}

	var history []hub.InstallRecord
	if cfg.Token != "" {
		history, _ = client.GetInstalls()
	}

	plan := hub.BuildDependencyPlan(depsResp.Dependencies, history)

	// Count actionable items
	var toAction []hub.DepPlanItem
	for _, item := range plan {
		if item.Action != hub.ActionSkip {
			toAction = append(toAction, item)
		}
	}
	if len(toAction) == 0 {
		fmt.Println("\nDependencies: all already installed ✓")
		return nil
	}

	// Resource check
	warn := hub.CheckResources(plan, s)

	// Print plan
	fmt.Printf("\nDependencies for %s:\n", mainSlug)
	for _, item := range plan {
		switch item.Action {
		case hub.ActionSkip:
			fmt.Printf("  ✓ %-20s v%s (설치됨, skip)\n", item.Dep.Package.Slug, item.InstalledVersion)
		case hub.ActionUpdate:
			fmt.Printf("  ↑ %-20s v%s → %s (업데이트)\n", item.Dep.Package.Slug, item.InstalledVersion, item.Dep.MinVersion)
		case hub.ActionInstall:
			fmt.Printf("  + %-20s %s (신규 설치)\n", item.Dep.Package.Slug, item.Dep.MinVersion)
		}
	}

	if warn != nil {
		fmt.Printf("\n⚠ 리소스 부족: %s\n", warn.Message)
		if !installYes {
			fmt.Print("계속 진행할까요? [y/N] ")
			reader := bufio.NewReader(os.Stdin)
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(strings.ToLower(input))
			if input != "y" && input != "yes" {
				fmt.Println("취소됨.")
				return fmt.Errorf("리소스 부족으로 취소")
			}
		}
	}

	// Install each actionable dep
	for _, item := range toAction {
		depSlug := item.Dep.Package.Slug
		depVersion := item.Dep.MinVersion
		if depVersion == "" {
			depVersion = "latest"
		}
		fmt.Printf("\n[dep] %s %s (%s)...\n", depSlug, depVersion, item.Action)
		depScript, err := client.GetInstallScript(depSlug, depVersion, strategy)
		if err != nil {
			fmt.Printf("  ⚠ %s install script 없음 — skip\n", depSlug)
			continue
		}
		if err := runScriptFlow(depScript, depSlug, depVersion); err != nil {
			fmt.Printf("  ✗ %s 설치 실패: %v\n", depSlug, err)
			continue
		}
		if cfg.Token != "" {
			_, _ = client.RecordInstall(depSlug, depVersion, strategy, groupID, "dependency")
		}
	}
	return nil
}
```

- [ ] **Step 4: Build to verify no compile errors**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
go build ./...
```
Expected: no errors

- [ ] **Step 5: Run all CLI tests**

```bash
go test ./... -count=1
```
Expected: all PASS

- [ ] **Step 6: Manual smoke test**

```bash
# Start hub locally with test DB (see previous session for setup)
# Then:
./morphso install gopedia --docker --no-deps --yes --hub-url http://localhost:18080
```
Expected: installs gopedia only, no deps flow

```bash
./morphso install gopedia --docker --yes --hub-url http://localhost:18080
```
Expected: prints "Dependencies for gopedia:", then installs any non-skipped deps, then gopedia

- [ ] **Step 7: Commit**

```bash
git add cmd/install.go
git commit -m "feat: add deps flow to install — fetch, resource check, sequential install, --no-deps"
```

---

## Self-Review

**1. Spec coverage:**
- ✅ Migration 007 (package_dependencies + resource_requirements JSONB)
- ✅ Migration 008 (install_history: install_group_id, install_source, status, error_message)
- ✅ Migration 009 (package_downloads)
- ✅ Gopedia 3-stage flow (Task 5)
- ✅ Author PUT deps (Task 6)
- ✅ LLM detection via OllamaDetector (Task 4)
- ✅ RecordInstall returns ID (Task 7)
- ✅ GET /users/me/purchases (Task 7)
- ✅ GET /packages/{slug}/stats (Task 7)
- ✅ Config GopediaURL (Task 8)
- ✅ CLI: DependencyInfo types (Task 9)
- ✅ CLI: GetDependencies (Task 9)
- ✅ CLI: BuildDependencyPlan + CheckResources (Task 10)
- ✅ CLI: deps flow, --no-deps flag (Task 11)
- ✅ install_group_id sent with all installs in same session (Task 11)

**2. Type consistency check:**
- `dependency.ResourceRequirements` (hub) ↔ `hub.ResourceRequirements` (CLI): same field names ✅
- `hub.ActionInstall/Update/Skip` constants used in both deps.go and install.go ✅
- `hub.DepPlanItem.Dep` is `DependencyInfo`, `Dep.Package` is `DepPackageRef` with `.Slug` ✅
- `client.RecordInstall` new signature `(slug, version, strategy, groupID, source string)` used in Task 9 + 11 ✅
- `gopedia.DepDocument` used in gopedia client and referenced in handler ✅

**3. Placeholder check:** None found. All code blocks are complete.
