# moso install LLM-powered Script Generation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** hub가 LLM(Claude/Ollama)으로 install script를 생성·캐시하고, CLI가 fetch → preview → confirm → sh 실행하는 end-to-end 흐름을 구현한다.

**Architecture:** morphso-hub에 `install_scripts`(캐시) + `llm_usage`(일별 카운트) 테이블 추가. `GET /packages/{slug}/install-script`가 캐시 히트 시 즉시 반환, 미스 시 Claude(Pro 또는 Free <10회) 또는 Ollama(Free ≥10회)로 생성 후 캐시 저장. Tier는 JWT claim `morphso_tier`에서 판독 (DB 조회 불필요). morphso CLI는 `@version` 파싱, hub script fetch, preview/confirm 후 sh 실행으로 전환.

**Tech Stack:** Go 1.26, chi v5, pgx/v5 (hub), cobra (CLI), Anthropic Messages API via net/http, Ollama API via net/http, crypto/sha256 (stdlib)

---

## File Structure

### morphso-hub (`/Users/dong-hoshin/Documents/dev/morphso-hub/`)

**Create:**
- `internal/db/migrations/005_install_scripts.up.sql`
- `internal/db/migrations/006_llm_usage.up.sql`
- `internal/installscript/domain.go` — InstallScript, ScriptResponse 타입, ErrNotFound
- `internal/installscript/store.go` — Store 인터페이스 + pgStore 구현
- `internal/installscript/llm.go` — LLMService 인터페이스, ClaudeClient, OllamaClient, prompt builder
- `internal/installscript/llm_test.go`
- `internal/api/handlers/install_scripts.go`
- `internal/api/handlers/install_scripts_test.go`

**Modify:**
- `internal/config/config.go` — ClaudeAPIKey, OllamaURL, OllamaModel 필드 추가
- `internal/api/router.go` — scriptHandler 파라미터 + 라우트 3개 추가
- `cmd/server/main.go` — LLM clients + scriptHandler 와이어업

### morphso CLI (`/Users/dong-hoshin/Documents/dev/morphso/`)

**Create:**
- `internal/hub/parse.go` — ParseSlugVersion 함수
- `internal/hub/parse_test.go`
- `internal/hub/install_script_test.go`

**Modify:**
- `internal/hub/types.go` — InstallScript 타입 추가
- `internal/hub/client.go` — GetInstallScript, GetInstallTemplate 메서드 추가
- `cmd/install.go` — @version 파싱, --template/--config 플래그, 설치 흐름 교체

---

## Task 1: DB Migrations

**Files:**
- Create: `internal/db/migrations/005_install_scripts.up.sql`
- Create: `internal/db/migrations/006_llm_usage.up.sql`

- [ ] **Step 1: 005_install_scripts.up.sql 작성**

`/Users/dong-hoshin/Documents/dev/morphso-hub/internal/db/migrations/005_install_scripts.up.sql`:
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

- [ ] **Step 2: 006_llm_usage.up.sql 작성**

`/Users/dong-hoshin/Documents/dev/morphso-hub/internal/db/migrations/006_llm_usage.up.sql`:
```sql
CREATE TABLE llm_usage (
    user_id    TEXT NOT NULL,
    date       DATE NOT NULL DEFAULT CURRENT_DATE,
    count      INT  NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, date)
);
```

- [ ] **Step 3: Commit**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
git add internal/db/migrations/005_install_scripts.up.sql internal/db/migrations/006_llm_usage.up.sql
git commit -m "feat: add install_scripts and llm_usage migrations"
```

---

## Task 2: installscript domain + store

**Files:**
- Create: `internal/installscript/domain.go`
- Create: `internal/installscript/store.go`

- [ ] **Step 1: domain.go 작성**

`/Users/dong-hoshin/Documents/dev/morphso-hub/internal/installscript/domain.go`:
```go
package installscript

import (
	"errors"
	"time"
)

var ErrNotFound = errors.New("script not found")

type InstallScript struct {
	ID             string
	PackageSlug    string
	Version        string
	Strategy       string
	Script         string
	ConfigTemplate string
	SHA256         string
	CreatedAt      time.Time
}

type ScriptResponse struct {
	Script      string `json:"script"`
	SHA256      string `json:"sha256"`
	Version     string `json:"version"`
	HasTemplate bool   `json:"has_template"`
	ModelUsed   string `json:"model_used"`
}
```

- [ ] **Step 2: store.go 작성**

`/Users/dong-hoshin/Documents/dev/morphso-hub/internal/installscript/store.go`:
```go
package installscript

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store interface {
	GetScript(ctx context.Context, slug, version, strategy string) (*InstallScript, error)
	SaveScript(ctx context.Context, s *InstallScript) error
	GetDailyCount(ctx context.Context, userID string) (int, error)
	IncrDailyCount(ctx context.Context, userID string) error
}

type pgStore struct {
	pool *pgxpool.Pool
}

func NewPGStore(pool *pgxpool.Pool) Store {
	return &pgStore{pool: pool}
}

func (s *pgStore) GetScript(ctx context.Context, slug, version, strategy string) (*InstallScript, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, package_slug, version, strategy, script, config_template, sha256, created_at
		FROM install_scripts
		WHERE package_slug = $1 AND version = $2 AND strategy = $3`,
		slug, version, strategy)
	var is InstallScript
	err := row.Scan(&is.ID, &is.PackageSlug, &is.Version, &is.Strategy,
		&is.Script, &is.ConfigTemplate, &is.SHA256, &is.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan install_script: %w", err)
	}
	return &is, nil
}

func (s *pgStore) SaveScript(ctx context.Context, is *InstallScript) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO install_scripts (package_slug, version, strategy, script, config_template, sha256)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (package_slug, version, strategy)
		DO UPDATE SET script = EXCLUDED.script,
		              config_template = EXCLUDED.config_template,
		              sha256 = EXCLUDED.sha256`,
		is.PackageSlug, is.Version, is.Strategy, is.Script, is.ConfigTemplate, is.SHA256)
	if err != nil {
		return fmt.Errorf("save install_script: %w", err)
	}
	return nil
}

func (s *pgStore) GetDailyCount(ctx context.Context, userID string) (int, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT count FROM llm_usage WHERE user_id = $1 AND date = CURRENT_DATE`, userID)
	var count int
	err := row.Scan(&count)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("get daily count: %w", err)
	}
	return count, nil
}

func (s *pgStore) IncrDailyCount(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO llm_usage (user_id, date, count) VALUES ($1, CURRENT_DATE, 1)
		ON CONFLICT (user_id, date) DO UPDATE SET count = llm_usage.count + 1`,
		userID)
	if err != nil {
		return fmt.Errorf("incr daily count: %w", err)
	}
	return nil
}
```

- [ ] **Step 3: 컴파일 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub && go build ./internal/installscript/...
```
Expected: 출력 없음 (정상 컴파일)

- [ ] **Step 4: Commit**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
git add internal/installscript/domain.go internal/installscript/store.go
git commit -m "feat: add installscript domain types and PG store"
```

---

## Task 3: LLM service (Claude + Ollama)

**Files:**
- Create: `internal/installscript/llm.go`
- Create: `internal/installscript/llm_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

`/Users/dong-hoshin/Documents/dev/morphso-hub/internal/installscript/llm_test.go`:
```go
package installscript_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tojiuni/morphso-hub/internal/domain"
	"github.com/tojiuni/morphso-hub/internal/installscript"
)

var testPkg = &domain.Package{Name: "Gopedia", Slug: "gopedia", Type: domain.TypeMCP}

func TestClaudeClient_GenerateInstallScript(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "test-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": "#!/bin/sh\ndocker run gopedia:latest"}},
		})
	}))
	defer srv.Close()

	client := installscript.NewClaudeClient("test-key", installscript.WithClaudeURL(srv.URL))
	script, err := client.GenerateInstallScript(testPkg, "latest", "docker")
	require.NoError(t, err)
	assert.Contains(t, script, "#!/bin/sh")
}

func TestOllamaClient_GenerateInstallScript(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/generate", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"response": "#!/bin/sh\ndocker run gopedia:latest",
		})
	}))
	defer srv.Close()

	client := installscript.NewOllamaClient(srv.URL, "llama3.2")
	script, err := client.GenerateInstallScript(testPkg, "latest", "docker")
	require.NoError(t, err)
	assert.Contains(t, script, "#!/bin/sh")
}
```

- [ ] **Step 2: 테스트 실패 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub && go test ./internal/installscript/... -count=1 2>&1 | head -20
```
Expected: FAIL — `installscript.NewClaudeClient undefined`

- [ ] **Step 3: llm.go 작성**

`/Users/dong-hoshin/Documents/dev/morphso-hub/internal/installscript/llm.go`:
```go
package installscript

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/tojiuni/morphso-hub/internal/domain"
)

// LLMService generates a POSIX install script for a package.
type LLMService interface {
	GenerateInstallScript(pkg *domain.Package, version, strategy string) (string, error)
}

func buildPrompt(pkg *domain.Package, version, strategy string) string {
	return fmt.Sprintf(
		`Write a POSIX shell script to install "%s" (slug: %s, type: %s) version %s using the %s strategy.
Requirements:
- First line must be #!/bin/sh
- Works on macOS and Linux without modification
- docker strategy: docker pull <image> then docker run -d --name %s <image>
- If the env variable MOSO_CONFIG is set, source it before running: [ -n "$MOSO_CONFIG" ] && . "$MOSO_CONFIG"
- Idempotent where possible (re-running should not fail)
- Minimal output
Output ONLY the shell script. No explanation, no markdown fences.`,
		pkg.Name, pkg.Slug, string(pkg.Type), version, strategy, pkg.Slug)
}

// --- Claude client ---

const defaultClaudeURL = "https://api.anthropic.com/v1/messages"
const claudeModel = "claude-haiku-4-5-20251001"

type claudeRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	Messages  []claudeMessage `json:"messages"`
}

type claudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type claudeResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
}

type ClaudeClient struct {
	apiKey string
	apiURL string
	http   *http.Client
}

type ClaudeOption func(*ClaudeClient)

func WithClaudeURL(url string) ClaudeOption {
	return func(c *ClaudeClient) { c.apiURL = url }
}

func NewClaudeClient(apiKey string, opts ...ClaudeOption) *ClaudeClient {
	c := &ClaudeClient{apiKey: apiKey, apiURL: defaultClaudeURL, http: &http.Client{}}
	for _, o := range opts {
		o(c)
	}
	return c
}

func (c *ClaudeClient) GenerateInstallScript(pkg *domain.Package, version, strategy string) (string, error) {
	body := claudeRequest{
		Model:     claudeModel,
		MaxTokens: 1024,
		Messages:  []claudeMessage{{Role: "user", Content: buildPrompt(pkg, version, strategy)}},
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return "", fmt.Errorf("encode claude request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.apiURL, &buf)
	if err != nil {
		return "", fmt.Errorf("create claude request: %w", err)
	}
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("claude http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("claude api returned %s", resp.Status)
	}
	var result claudeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode claude response: %w", err)
	}
	if len(result.Content) == 0 {
		return "", fmt.Errorf("claude returned empty content")
	}
	return result.Content[0].Text, nil
}

// --- Ollama client ---

type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type ollamaResponse struct {
	Response string `json:"response"`
}

type OllamaClient struct {
	baseURL string
	model   string
	http    *http.Client
}

func NewOllamaClient(baseURL, model string) *OllamaClient {
	return &OllamaClient{baseURL: baseURL, model: model, http: &http.Client{}}
}

func (c *OllamaClient) GenerateInstallScript(pkg *domain.Package, version, strategy string) (string, error) {
	body := ollamaRequest{Model: c.model, Prompt: buildPrompt(pkg, version, strategy), Stream: false}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return "", fmt.Errorf("encode ollama request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/generate", &buf)
	if err != nil {
		return "", fmt.Errorf("create ollama request: %w", err)
	}
	req.Header.Set("content-type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama api returned %s", resp.Status)
	}
	var result ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode ollama response: %w", err)
	}
	return result.Response, nil
}
```

- [ ] **Step 4: 테스트 통과 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub && go test ./internal/installscript/... -count=1 -v
```
Expected: `TestClaudeClient_GenerateInstallScript PASS`, `TestOllamaClient_GenerateInstallScript PASS`

- [ ] **Step 5: Commit**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
git add internal/installscript/llm.go internal/installscript/llm_test.go
git commit -m "feat: add Claude and Ollama LLM service implementations"
```

---

## Task 4: install_scripts HTTP handlers

**Files:**
- Create: `internal/api/handlers/install_scripts.go`
- Create: `internal/api/handlers/install_scripts_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

`/Users/dong-hoshin/Documents/dev/morphso-hub/internal/api/handlers/install_scripts_test.go`:
```go
package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tojiuni/morphso-hub/internal/api/handlers"
	"github.com/tojiuni/morphso-hub/internal/domain"
	"github.com/tojiuni/morphso-hub/internal/installscript"
	"github.com/tojiuni/morphso-hub/internal/registry"
)

// --- mock stores ---

type mockScriptStore struct {
	script *installscript.InstallScript
	getErr error
	count  int
}

func (m *mockScriptStore) GetScript(_ context.Context, _, _, _ string) (*installscript.InstallScript, error) {
	return m.script, m.getErr
}
func (m *mockScriptStore) SaveScript(_ context.Context, s *installscript.InstallScript) error {
	m.script = s
	return nil
}
func (m *mockScriptStore) GetDailyCount(_ context.Context, _ string) (int, error) { return m.count, nil }
func (m *mockScriptStore) IncrDailyCount(_ context.Context, _ string) error        { m.count++; return nil }

type mockPkgStore struct {
	pkg *domain.Package
	err error
}

func (m *mockPkgStore) Create(_ context.Context, _ *domain.Package) error { return nil }
func (m *mockPkgStore) GetBySlug(_ context.Context, _ string) (*domain.Package, error) {
	return m.pkg, m.err
}
func (m *mockPkgStore) Search(_ context.Context, _ string, _, _ int) ([]*domain.Package, error) {
	return nil, nil
}

type mockLLM struct{ script string }

func (m *mockLLM) GenerateInstallScript(_ *domain.Package, _, _ string) (string, error) {
	return m.script, nil
}

// --- helper ---

func newRouter(h *handlers.InstallScriptHandler) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/packages/{slug}/install-script", h.GetInstallScript)
	r.Get("/packages/{slug}/install-script/template", h.GetInstallTemplate)
	r.Put("/packages/{slug}/install-script", h.PutInstallScript)
	return r
}

// --- tests ---

func TestGetInstallScript_CacheHit(t *testing.T) {
	cached := &installscript.InstallScript{
		PackageSlug: "gopedia", Version: "latest", Strategy: "docker",
		Script: "#!/bin/sh\ndocker run gopedia", SHA256: "sha256:abc",
		CreatedAt: time.Now(),
	}
	h := handlers.NewInstallScriptHandler(&mockScriptStore{script: cached}, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/packages/gopedia/install-script?version=latest&strategy=docker", nil)
	rw := httptest.NewRecorder()
	newRouter(h).ServeHTTP(rw, req)

	require.Equal(t, http.StatusOK, rw.Code)
	var resp installscript.ScriptResponse
	require.NoError(t, json.NewDecoder(rw.Body).Decode(&resp))
	assert.Equal(t, "#!/bin/sh\ndocker run gopedia", resp.Script)
	assert.Equal(t, "", resp.ModelUsed)
}

func TestGetInstallScript_CacheMiss_CallsLLM(t *testing.T) {
	llm := &mockLLM{script: "#!/bin/sh\ndocker run gopedia:latest"}
	store := &mockScriptStore{getErr: installscript.ErrNotFound}
	pkgs := &mockPkgStore{pkg: &domain.Package{Slug: "gopedia", Name: "Gopedia"}}
	h := handlers.NewInstallScriptHandler(store, pkgs, nil, llm)

	req := httptest.NewRequest(http.MethodGet, "/packages/gopedia/install-script?strategy=docker", nil)
	rw := httptest.NewRecorder()
	newRouter(h).ServeHTTP(rw, req)

	require.Equal(t, http.StatusOK, rw.Code)
	var resp installscript.ScriptResponse
	require.NoError(t, json.NewDecoder(rw.Body).Decode(&resp))
	assert.Contains(t, resp.Script, "#!/bin/sh")
	assert.NotEmpty(t, resp.SHA256)
	assert.NotNil(t, store.script, "script should be saved to cache")
}

func TestGetInstallScript_PackageNotFound(t *testing.T) {
	store := &mockScriptStore{getErr: installscript.ErrNotFound}
	pkgs := &mockPkgStore{err: registry.ErrNotFound}
	h := handlers.NewInstallScriptHandler(store, pkgs, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/packages/unknown/install-script", nil)
	rw := httptest.NewRecorder()
	newRouter(h).ServeHTTP(rw, req)

	assert.Equal(t, http.StatusNotFound, rw.Code)
}

func TestPutInstallScript_RequiresAuth(t *testing.T) {
	h := handlers.NewInstallScriptHandler(&mockScriptStore{}, &mockPkgStore{}, nil, nil)
	body := strings.NewReader(`{"strategy":"docker","script":"#!/bin/sh\necho hi"}`)
	req := httptest.NewRequest(http.MethodPut, "/packages/gopedia/install-script", body)
	rw := httptest.NewRecorder()
	newRouter(h).ServeHTTP(rw, req)

	assert.Equal(t, http.StatusUnauthorized, rw.Code)
}

func TestGetInstallTemplate_NoTemplate(t *testing.T) {
	cached := &installscript.InstallScript{
		PackageSlug: "gopedia", Version: "latest", Strategy: "docker",
		Script: "#!/bin/sh\ndocker run gopedia", SHA256: "sha256:abc",
		ConfigTemplate: "",
	}
	h := handlers.NewInstallScriptHandler(&mockScriptStore{script: cached}, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/packages/gopedia/install-script/template?strategy=docker", nil)
	rw := httptest.NewRecorder()
	newRouter(h).ServeHTTP(rw, req)

	assert.Equal(t, http.StatusNotFound, rw.Code)
}
```

- [ ] **Step 2: 테스트 실패 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub && go test ./internal/api/handlers/... -run "TestGetInstallScript|TestPutInstallScript|TestGetInstallTemplate" -count=1 2>&1 | head -20
```
Expected: FAIL — `handlers.NewInstallScriptHandler undefined`

- [ ] **Step 3: install_scripts.go 작성**

`/Users/dong-hoshin/Documents/dev/morphso-hub/internal/api/handlers/install_scripts.go`:
```go
package handlers

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/tojiuni/morphso-hub/internal/auth"
	"github.com/tojiuni/morphso-hub/internal/domain"
	"github.com/tojiuni/morphso-hub/internal/installscript"
	"github.com/tojiuni/morphso-hub/internal/registry"
)

type InstallScriptHandler struct {
	scripts  installscript.Store
	packages registry.Store
	claude   installscript.LLMService
	ollama   installscript.LLMService
}

// NewInstallScriptHandler creates the handler. packages may be nil only when scripts store always hits cache.
// claude may be nil (falls back to ollama for all tiers).
func NewInstallScriptHandler(
	scripts installscript.Store,
	packages registry.Store,
	claude installscript.LLMService,
	ollama installscript.LLMService,
) *InstallScriptHandler {
	return &InstallScriptHandler{scripts: scripts, packages: packages, claude: claude, ollama: ollama}
}

func (h *InstallScriptHandler) GetInstallScript(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	version := r.URL.Query().Get("version")
	if version == "" {
		version = "latest"
	}
	strategy := r.URL.Query().Get("strategy")
	if strategy == "" {
		strategy = "docker"
	}

	// Cache hit — return immediately, no LLM cost.
	existing, err := h.scripts.GetScript(r.Context(), slug, version, strategy)
	if err == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(installscript.ScriptResponse{
			Script:      existing.Script,
			SHA256:      existing.SHA256,
			Version:     existing.Version,
			HasTemplate: existing.ConfigTemplate != "",
			ModelUsed:   "",
		})
		return
	}
	if !errors.Is(err, installscript.ErrNotFound) {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Package must exist before generating.
	pkg, err := h.packages.GetBySlug(r.Context(), slug)
	if errors.Is(err, registry.ErrNotFound) {
		http.Error(w, "package not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	llm, modelUsed := h.pickLLM(r)
	script, err := llm.GenerateInstallScript(pkg, version, strategy)
	if err != nil {
		http.Error(w, "script generation failed", http.StatusServiceUnavailable)
		return
	}

	hash := sha256.Sum256([]byte(script))
	s := &installscript.InstallScript{
		PackageSlug: slug,
		Version:     version,
		Strategy:    strategy,
		Script:      script,
		SHA256:      fmt.Sprintf("sha256:%x", hash),
	}
	_ = h.scripts.SaveScript(r.Context(), s)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(installscript.ScriptResponse{
		Script:    script,
		SHA256:    s.SHA256,
		Version:   version,
		ModelUsed: modelUsed,
	})
}

// pickLLM selects Claude or Ollama based on JWT tier and daily usage.
// Pro tier → always Claude. Free < 10/day → Claude (and increments counter). Otherwise → Ollama.
func (h *InstallScriptHandler) pickLLM(r *http.Request) (installscript.LLMService, string) {
	if h.claude == nil {
		return h.ollama, "ollama"
	}
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		return h.ollama, "ollama"
	}
	if claims.Tier == domain.TierPro {
		return h.claude, "claude"
	}
	count, _ := h.scripts.GetDailyCount(r.Context(), claims.UserID)
	if count < 10 {
		_ = h.scripts.IncrDailyCount(r.Context(), claims.UserID)
		return h.claude, "claude"
	}
	return h.ollama, "ollama"
}

func (h *InstallScriptHandler) PutInstallScript(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		Version        string `json:"version"`
		Strategy       string `json:"strategy"`
		Script         string `json:"script"`
		ConfigTemplate string `json:"config_template"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Script == "" || req.Strategy == "" {
		http.Error(w, "script and strategy are required", http.StatusBadRequest)
		return
	}
	if req.Version == "" {
		req.Version = "latest"
	}
	hash := sha256.Sum256([]byte(req.Script))
	s := &installscript.InstallScript{
		PackageSlug:    slug,
		Version:        req.Version,
		Strategy:       req.Strategy,
		Script:         req.Script,
		ConfigTemplate: req.ConfigTemplate,
		SHA256:         fmt.Sprintf("sha256:%x", hash),
	}
	if err := h.scripts.SaveScript(r.Context(), s); err != nil {
		http.Error(w, "save failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *InstallScriptHandler) GetInstallTemplate(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	strategy := r.URL.Query().Get("strategy")
	if strategy == "" {
		strategy = "docker"
	}
	s, err := h.scripts.GetScript(r.Context(), slug, "latest", strategy)
	if errors.Is(err, installscript.ErrNotFound) {
		http.Error(w, "template not available", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if s.ConfigTemplate == "" {
		http.Error(w, "no template available", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte(s.ConfigTemplate))
}
```

- [ ] **Step 4: 테스트 통과 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub && go test ./internal/api/handlers/... -run "TestGetInstallScript|TestPutInstallScript|TestGetInstallTemplate" -count=1 -v
```
Expected: 5개 테스트 모두 PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
git add internal/api/handlers/install_scripts.go internal/api/handlers/install_scripts_test.go
git commit -m "feat: add install script GET/PUT/template handlers"
```

---

## Task 5: Config + Router + Wire up (hub)

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/api/router.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: config.go에 LLM 필드 추가**

`internal/config/config.go`의 `Config` struct에 추가:
```go
ClaudeAPIKey string
OllamaURL    string
OllamaModel  string
```

`Load()` 함수의 return 구문에 추가 (기존 StripeSecretKey 아래):
```go
ClaudeAPIKey: os.Getenv("CLAUDE_API_KEY"),
OllamaURL:    getEnvOrDefault("OLLAMA_URL", "http://localhost:11434"),
OllamaModel:  getEnvOrDefault("OLLAMA_MODEL", "llama3.2"),
```

파일 끝에 헬퍼 함수 추가:
```go
func getEnvOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
```

- [ ] **Step 2: router.go에 scriptHandler 파라미터 + 라우트 추가**

`internal/api/router.go`의 `NewRouter` 시그니처 변경:
```go
func NewRouter(
	pool *pgxpool.Pool,
	store registry.Store,
	authMW *auth.Middleware,
	engine *recommend.Engine,
	billingStore billing.Store,
	stripeService billing.StripeService,
	scriptHandler *handlers.InstallScriptHandler, // 추가
) *chi.Mux {
```

optional-auth 그룹에 라우트 2개 추가 (기존 `/packages/{slug}` 아래):
```go
r.Get("/packages/{slug}/install-script", scriptHandler.GetInstallScript)
r.Get("/packages/{slug}/install-script/template", scriptHandler.GetInstallTemplate)
```

required-auth 그룹에 라우트 1개 추가 (기존 `r.Post("/packages", ...)` 아래):
```go
r.Put("/packages/{slug}/install-script", scriptHandler.PutInstallScript)
```

- [ ] **Step 3: main.go에 LLM 와이어업 추가**

`cmd/server/main.go`의 import에 추가:
```go
"github.com/tojiuni/morphso-hub/internal/installscript"
```

`stripeService` 설정 블록 이후에 추가:
```go
scriptStore := installscript.NewPGStore(pool)

var claudeLLM installscript.LLMService
if cfg.ClaudeAPIKey != "" {
    claudeLLM = installscript.NewClaudeClient(cfg.ClaudeAPIKey)
    log.Print("llm: Claude API enabled")
} else {
    log.Print("llm: CLAUDE_API_KEY not set — all tiers use Ollama")
}
ollamaLLM := installscript.NewOllamaClient(cfg.OllamaURL, cfg.OllamaModel)

scriptHandler := handlers.NewInstallScriptHandler(scriptStore, store, claudeLLM, ollamaLLM)
```

`api.NewRouter(...)` 호출에 `scriptHandler` 인수 추가:
```go
router := api.NewRouter(pool, store, authMW, engine, billingStore, stripeService, scriptHandler)
```

- [ ] **Step 4: 전체 빌드 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub && go build ./...
```
Expected: 출력 없음 (정상 빌드)

- [ ] **Step 5: 전체 테스트 실행**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub && go test ./... -count=1
```
Expected: 모든 테스트 PASS

- [ ] **Step 6: Commit**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-hub
git add internal/config/config.go internal/api/router.go cmd/server/main.go
git commit -m "feat: wire up LLM script generation in hub server"
```

---

## Task 6: CLI — ParseSlugVersion + hub client 메서드

**Files:**
- Create: `internal/hub/parse.go`
- Create: `internal/hub/parse_test.go`
- Modify: `internal/hub/types.go`
- Modify: `internal/hub/client.go`
- Create: `internal/hub/install_script_test.go`

- [ ] **Step 1: parse_test.go 작성 (실패하는 테스트)**

`/Users/dong-hoshin/Documents/dev/morphso/internal/hub/parse_test.go`:
```go
package hub_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tojiuni/morphso/internal/hub"
)

func TestParseSlugVersion(t *testing.T) {
	tests := []struct {
		input   string
		slug    string
		version string
	}{
		{"gopedia", "gopedia", "latest"},
		{"gopedia@1.2.3", "gopedia", "1.2.3"},
		{"gopedia@latest", "gopedia", "latest"},
		{"my-pkg@v2.0.0", "my-pkg", "v2.0.0"},
		{"a@b@c", "a@b", "c"},
	}
	for _, tt := range tests {
		slug, version := hub.ParseSlugVersion(tt.input)
		assert.Equal(t, tt.slug, slug, "slug for %q", tt.input)
		assert.Equal(t, tt.version, version, "version for %q", tt.input)
	}
}
```

- [ ] **Step 2: 테스트 실패 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso && go test ./internal/hub/... -run TestParseSlugVersion -count=1 2>&1 | head -10
```
Expected: FAIL — `hub.ParseSlugVersion undefined`

- [ ] **Step 3: parse.go 작성**

`/Users/dong-hoshin/Documents/dev/morphso/internal/hub/parse.go`:
```go
package hub

import "strings"

// ParseSlugVersion splits "gopedia@1.2.3" → ("gopedia", "1.2.3").
// Returns ("slug", "latest") when no @ suffix is present.
func ParseSlugVersion(arg string) (slug, version string) {
	if idx := strings.LastIndex(arg, "@"); idx != -1 {
		return arg[:idx], arg[idx+1:]
	}
	return arg, "latest"
}
```

- [ ] **Step 4: parse 테스트 통과 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso && go test ./internal/hub/... -run TestParseSlugVersion -count=1 -v
```
Expected: `TestParseSlugVersion PASS`

- [ ] **Step 5: install_script_test.go 작성 (실패하는 테스트)**

`/Users/dong-hoshin/Documents/dev/morphso/internal/hub/install_script_test.go`:
```go
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

func TestGetInstallScript(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/packages/gopedia/install-script", r.URL.Path)
		assert.Equal(t, "1.2.3", r.URL.Query().Get("version"))
		assert.Equal(t, "docker", r.URL.Query().Get("strategy"))
		json.NewEncoder(w).Encode(map[string]any{
			"script":       "#!/bin/sh\ndocker run gopedia:1.2.3",
			"sha256":       "sha256:abc123",
			"version":      "1.2.3",
			"has_template": false,
			"model_used":   "claude",
		})
	}))
	defer srv.Close()

	client := hub.NewClient(srv.URL, "token")
	result, err := client.GetInstallScript("gopedia", "1.2.3", "docker")
	require.NoError(t, err)
	assert.Equal(t, "#!/bin/sh\ndocker run gopedia:1.2.3", result.Script)
	assert.Equal(t, "sha256:abc123", result.SHA256)
	assert.Equal(t, "claude", result.ModelUsed)
}

func TestGetInstallTemplate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/packages/gopedia/install-script/template", r.URL.Path)
		assert.Equal(t, "docker", r.URL.Query().Get("strategy"))
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("REGISTRY=docker.io\nIMAGE_TAG=latest\n"))
	}))
	defer srv.Close()

	client := hub.NewClient(srv.URL, "token")
	tmpl, err := client.GetInstallTemplate("gopedia", "docker")
	require.NoError(t, err)
	assert.Contains(t, tmpl, "REGISTRY=")
}
```

- [ ] **Step 6: 테스트 실패 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso && go test ./internal/hub/... -run "TestGetInstallScript|TestGetInstallTemplate" -count=1 2>&1 | head -10
```
Expected: FAIL — `hub.(*Client).GetInstallScript undefined`

- [ ] **Step 7: types.go에 InstallScript 타입 추가**

`/Users/dong-hoshin/Documents/dev/morphso/internal/hub/types.go`의 기존 타입들 아래에 추가:
```go
type InstallScript struct {
	Script      string `json:"script"`
	SHA256      string `json:"sha256"`
	Version     string `json:"version"`
	HasTemplate bool   `json:"has_template"`
	ModelUsed   string `json:"model_used"`
}
```

- [ ] **Step 8: client.go에 GetInstallScript + GetInstallTemplate 추가**

`/Users/dong-hoshin/Documents/dev/morphso/internal/hub/client.go`의 `RecordInstall` 아래에 추가:
```go
func (c *Client) GetInstallScript(slug, version, strategy string) (*InstallScript, error) {
	path := fmt.Sprintf("/packages/%s/install-script?version=%s&strategy=%s",
		url.PathEscape(slug), url.QueryEscape(version), url.QueryEscape(strategy))
	req, err := c.newRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var result InstallScript
	if err := c.do(req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) GetInstallTemplate(slug, strategy string) (string, error) {
	path := fmt.Sprintf("/packages/%s/install-script/template?strategy=%s",
		url.PathEscape(slug), url.QueryEscape(strategy))
	req, err := c.newRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", ErrNotFound
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("server error: %s", resp.Status)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return "", fmt.Errorf("read template: %w", err)
	}
	return buf.String(), nil
}
```

Note: `bytes` package는 client.go에 이미 import되어 있음 (`newRequest`에서 사용).

- [ ] **Step 9: 테스트 통과 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso && go test ./internal/hub/... -count=1 -v
```
Expected: 모든 hub 패키지 테스트 PASS

- [ ] **Step 10: Commit**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
git add internal/hub/parse.go internal/hub/parse_test.go \
        internal/hub/types.go internal/hub/client.go \
        internal/hub/install_script_test.go
git commit -m "feat: add ParseSlugVersion, GetInstallScript, GetInstallTemplate to hub client"
```

---

## Task 7: CLI — install command 리팩터링

**Files:**
- Modify: `cmd/install.go`

- [ ] **Step 1: cmd/install.go 전체를 아래 내용으로 교체**

`/Users/dong-hoshin/Documents/dev/morphso/cmd/install.go`:
```go
package cmd

import (
	"bufio"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tojiuni/morphso/internal/config"
	"github.com/tojiuni/morphso/internal/hub"
	"github.com/tojiuni/morphso/internal/installer"
	"github.com/tojiuni/morphso/internal/spec"
)

var (
	installStrategy string
	installYes      bool
	installNative   bool
	installDocker   bool
	installK8s      bool
	installHelm     bool
	installTemplate bool
	installConfig   string
)

var installCmd = &cobra.Command{
	Use:   "install <package[@version]>",
	Short: "패키지 설치 (AI 추천 strategy 자동 선택)",
	Args:  cobra.ExactArgs(1),
	RunE:  runInstall,
}

func init() {
	installCmd.Flags().StringVar(&installStrategy, "strategy", "", "strategy 강제 지정 (native|docker|k8s|helm)")
	installCmd.Flags().BoolVar(&installYes, "yes", false, "비대화형 모드 (확인 스킵)")
	installCmd.Flags().BoolVar(&installNative, "native", false, "--strategy=native 단축키")
	installCmd.Flags().BoolVar(&installDocker, "docker", false, "--strategy=docker 단축키")
	installCmd.Flags().BoolVar(&installK8s, "k8s", false, "--strategy=k8s 단축키")
	installCmd.Flags().BoolVar(&installHelm, "helm", false, "--strategy=helm 단축키")
	installCmd.Flags().BoolVar(&installTemplate, "template", false, "config template을 ./<slug>.env로 저장")
	installCmd.Flags().StringVar(&installConfig, "config", "", "커스텀 config 파일 (MOSO_CONFIG 환경변수로 주입)")
	rootCmd.AddCommand(installCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	slug, version := hub.ParseSlugVersion(args[0])

	preferred := installStrategy
	if installNative {
		preferred = "native"
	} else if installDocker {
		preferred = "docker"
	} else if installK8s {
		preferred = "k8s"
	} else if installHelm {
		preferred = "helm"
	}

	cfg, err := config.DefaultLoad()
	if err != nil {
		return err
	}
	if hubURL != "" {
		cfg.HubURL = hubURL
	}

	// --template: download config template and exit early.
	if installTemplate {
		return runInstallTemplate(slug, preferred, cfg)
	}

	// 1. OS 스펙 수집
	home, _ := os.UserHomeDir()
	morphsoDir := filepath.Join(home, ".morphso")
	s, err := spec.GetOrCollect(morphsoDir)
	if err != nil {
		return fmt.Errorf("OS 스펙 수집 실패: %w", err)
	}

	// 2. 패키지 정보 조회
	client := hub.NewClient(cfg.HubURL, cfg.Token)
	pkg, err := client.GetPackage(slug)
	if errors.Is(err, hub.ErrNotFound) {
		return fmt.Errorf("패키지 '%s'를 찾을 수 없습니다", slug)
	}
	if err != nil {
		return fmt.Errorf("패키지 조회 실패: %w", err)
	}

	// 3. strategy 추천
	var strategy, reason string
	if cfg.Token != "" {
		result, err := client.Recommend(slug, s, preferred)
		if err == nil {
			strategy = result.Strategy
			reason = result.Reason
		}
	}
	if strategy == "" {
		strategy = hub.LocalRecommend(s, preferred)
		reason = "로컬 rule-based 추천 (hub 미연결 또는 미로그인)"
	}

	// 4. 추천 결과 출력 + strategy 확인
	fmt.Printf("\n추천: --%s\n", strategy)
	fmt.Printf("이유: %s\n\n", reason)

	if !installYes {
		fmt.Printf("진행하시겠습니까? [Y/n/native/docker/k8s/helm] ")
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))
		switch input {
		case "", "y", "yes":
		case "n", "no":
			fmt.Println("취소됨.")
			return nil
		case "native", "docker", "k8s", "helm":
			strategy = input
			fmt.Printf("strategy를 '%s'(으)로 변경합니다.\n", strategy)
		default:
			fmt.Println("취소됨.")
			return nil
		}
	}

	// 5. 필요 도구 확인
	if req := prerequisiteForStrategy(strategy, pkg.Type); req != "" && !installer.CheckPrerequisite(req) {
		return fmt.Errorf("'%s'가 설치되어 있지 않습니다. 먼저 설치하세요", req)
	}

	// 6. Hub에서 install script 조회 → 없으면 로컬 BuildCommand fallback
	installScript, err := client.GetInstallScript(slug, version, strategy)
	if err == nil {
		if err := runScriptFlow(installScript, slug, version); err != nil {
			return err
		}
		if cfg.Token != "" {
			_ = client.RecordInstall(slug, version, strategy)
		}
		return nil
	}
	if !errors.Is(err, hub.ErrNotFound) {
		return fmt.Errorf("install script 조회 실패: %w", err)
	}

	// Fallback: local BuildCommand (hub에 스크립트 미등록 패키지)
	command := installer.BuildCommand(pkg.Type, strategy, slug, version)
	fmt.Printf("\n실행: %s\n\n", strings.Join(command, " "))
	if err := installer.Run(command, os.Stdout); err != nil {
		return fmt.Errorf("설치 실패: %w", err)
	}
	if cfg.Token != "" {
		_ = client.RecordInstall(slug, version, strategy)
	}
	fmt.Printf("\n✓ '%s' 설치 완료!\n", slug)
	return nil
}

// runScriptFlow previews, confirms, and executes the hub-provided install script.
func runScriptFlow(script *hub.InstallScript, slug, version string) error {
	// SHA256 무결성 검증
	hash := sha256.Sum256([]byte(script.Script))
	computed := fmt.Sprintf("sha256:%x", hash)
	if computed != script.SHA256 {
		return fmt.Errorf("스크립트 무결성 오류: 예상 %s, 실제 %s", script.SHA256, computed)
	}

	// Preview + confirm
	if !installYes {
		fmt.Println("\n─── install script preview ─────────────────────────")
		fmt.Println(script.Script)
		fmt.Println("────────────────────────────────────────────────────")
		if script.ModelUsed != "" {
			fmt.Printf("(generated by: %s)\n\n", script.ModelUsed)
		}
		fmt.Print("Proceed? [Y/n] ")
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))
		if input == "n" || input == "no" {
			fmt.Println("취소됨.")
			return nil
		}
	}

	// 임시 파일에 스크립트 저장 후 실행
	tmpFile, err := os.CreateTemp("", fmt.Sprintf("moso-install-%s-*.sh", slug))
	if err != nil {
		return fmt.Errorf("임시 파일 생성 실패: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(script.Script); err != nil {
		tmpFile.Close()
		return fmt.Errorf("스크립트 쓰기 실패: %w", err)
	}
	tmpFile.Close()

	if err := os.Chmod(tmpFile.Name(), 0700); err != nil {
		return fmt.Errorf("chmod 실패: %w", err)
	}

	c := exec.Command("sh", tmpFile.Name())
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if installConfig != "" {
		c.Env = append(os.Environ(), "MOSO_CONFIG="+installConfig)
	}
	if err := c.Run(); err != nil {
		return fmt.Errorf("설치 실패: %w", err)
	}

	fmt.Printf("\n✓ '%s@%s' 설치 완료!\n", slug, version)
	return nil
}

// runInstallTemplate downloads the config template and saves it locally.
func runInstallTemplate(slug, strategy string, cfg *config.Config) error {
	if strategy == "" {
		strategy = "docker"
	}
	client := hub.NewClient(cfg.HubURL, cfg.Token)
	tmpl, err := client.GetInstallTemplate(slug, strategy)
	if errors.Is(err, hub.ErrNotFound) {
		return fmt.Errorf("'%s' 패키지에 %s config template이 없습니다", slug, strategy)
	}
	if err != nil {
		return fmt.Errorf("template 조회 실패: %w", err)
	}
	filename := slug + ".env"
	if err := os.WriteFile(filename, []byte(tmpl), 0644); err != nil {
		return fmt.Errorf("template 저장 실패: %w", err)
	}
	fmt.Printf("Config template saved to ./%s\n", filename)
	fmt.Printf("Edit it and re-run:\n  moso install %s --%s --config ./%s\n", slug, strategy, filename)
	return nil
}

func prerequisiteForStrategy(strategy, pkgType string) string {
	switch strategy {
	case "docker":
		return "docker"
	case "k8s", "helm":
		return "helm"
	case "native":
		switch pkgType {
		case "npm":
			return "npm"
		case "brew":
			return "brew"
		}
	}
	return ""
}
```

- [ ] **Step 2: 빌드 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso && go build ./...
```
Expected: 출력 없음 (정상 빌드)

- [ ] **Step 3: 전체 테스트 실행**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso && go test ./... -count=1
```
Expected: 모든 테스트 PASS

- [ ] **Step 4: Commit**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
git add cmd/install.go
git commit -m "feat: refactor install command — @version parsing, hub script fetch/preview/execute, --template/--config flags"
```

---

## 완료 후 검증

두 레포 모두 빌드·테스트 통과 후:

1. hub 배포 (ArgoCD 자동 sync 또는 `kubectl rollout restart deploy/morphso-hub -n morphso`)
2. DB 마이그레이션 확인: `kubectl -n morphso exec deploy/morphso-hub -- sh -c "...psql -c '\dt'"` → `install_scripts`, `llm_usage` 테이블 존재
3. CLI 수동 smoke test:
   ```bash
   moso install gopedia@latest --docker --yes
   # Expected: hub 호출 → LLM script 생성 → preview → 실행
   ```
4. 캐시 확인: 동일 명령 재실행 시 `model_used`가 없음 (캐시 히트)

---

## 미래 구현 예정 (이번 범위 외)

- `moso publish` 시 자동 install script 사전 생성
- Pro 구독 결제 (Stripe + Toss Payments 연동)
- config_template 편집 UI
- gopedia 패키지 morphso-hub에 첫 번째 패키지로 등록
