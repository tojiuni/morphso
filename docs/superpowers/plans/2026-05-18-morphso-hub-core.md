# morphso-hub Core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** morphso-hub Go API 서버 구축 — 패키지 레지스트리, ZITADEL 인증, OS 스펙 기반 AI 설치 추천, artifact-keeper 연동, 설치 이력 저장.

**Architecture:** chi 라우터 기반 단일 Go 바이너리. 도메인 타입 → store(DB) → handler(HTTP) 계층 분리. 외부 의존성(ZITADEL, artifact-keeper, Postgres, Redis)은 인터페이스로 추상화해 테스트에서 mock 사용.

**Tech Stack:** Go 1.23, chi v5, pgx/v5, golang-migrate, go-redis/v9, golang-jwt/jwt v5, testify v1

---

## 파일 구조

```
morphso-hub/
  cmd/server/main.go                  # 진입점, 의존성 조립
  internal/
    config/config.go                  # 환경변수 → Config 구조체
    db/
      postgres.go                     # pgx 커넥션 풀
      migrations/
        001_packages.sql
        002_install_history.sql
    domain/
      package.go                      # Package, PackageVersion, PackageStrategy 타입
      install.go                      # OSSpec, InstallPlan, InstallStep 타입
      user.go                         # UserClaims 타입
    registry/
      store.go                        # 패키지 메타데이터 DB 인터페이스 + 구현
      artifact.go                     # artifact-keeper HTTP 클라이언트
    recommend/
      engine.go                       # rule-based 스코어링 → 추천 strategy
    auth/
      middleware.go                   # ZITADEL JWKS 검증 미들웨어
    api/
      router.go                       # chi 라우터 조립
      handlers/
        health.go                     # GET /health
        packages.go                   # GET/POST /packages, GET /packages/:slug
        recommend.go                  # POST /recommend
        installs.go                   # POST /installs, GET /users/me/installs
  Dockerfile
  Makefile
  go.mod
```

---

## Task 1: 프로젝트 초기화

**Files:**
- Create: `morphso-hub/go.mod`
- Create: `morphso-hub/Makefile`
- Create: `morphso-hub/cmd/server/main.go`

- [ ] **Step 1: go 모듈 초기화**

```bash
cd morphso-hub
go mod init github.com/tojiuni/morphso-hub
```

- [ ] **Step 2: 의존성 추가**

```bash
go get github.com/go-chi/chi/v5@v5.2.1
go get github.com/jackc/pgx/v5@v5.7.2
go get github.com/redis/go-redis/v9@v9.7.3
go get github.com/golang-jwt/jwt/v5@v5.2.2
go get github.com/golang-migrate/migrate/v4@v4.18.1
go get github.com/golang-migrate/migrate/v4/database/pgx/v5
go get github.com/golang-migrate/migrate/v4/source/file
go get github.com/stretchr/testify@v1.10.0
go get github.com/stripe/stripe-go/v82@v82.0.0
```

- [ ] **Step 3: Makefile 생성**

```makefile
.PHONY: build test run lint migrate

build:
	go build -o bin/morphso-hub ./cmd/server

test:
	go test ./... -v -race

run:
	go run ./cmd/server

lint:
	golangci-lint run ./...

migrate-up:
	migrate -path internal/db/migrations -database "$$DATABASE_URL" up

migrate-down:
	migrate -path internal/db/migrations -database "$$DATABASE_URL" down 1
```

- [ ] **Step 4: 최소 main.go 생성**

```go
// cmd/server/main.go
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("morphso-hub listening on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 5: 빌드 확인**

```bash
make build
```
Expected: `bin/morphso-hub` 생성, 오류 없음

- [ ] **Step 6: 커밋**

```bash
git add go.mod go.sum Makefile cmd/
git commit -m "chore: initialize morphso-hub go module"
```

---

## Task 2: Config 로딩

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

```go
// internal/config/config_test.go
package config_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tojiuni/morphso-hub/internal/config"
)

func TestLoad_RequiredFields(t *testing.T) {
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost/morphso")
	os.Setenv("ZITADEL_ISSUER", "https://auth.toji.homes")
	os.Setenv("ARTIFACT_KEEPER_URL", "https://artifacts.toji.homes")
	os.Setenv("ARTIFACT_KEEPER_TOKEN", "token123")
	t.Cleanup(func() {
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("ZITADEL_ISSUER")
		os.Unsetenv("ARTIFACT_KEEPER_URL")
		os.Unsetenv("ARTIFACT_KEEPER_TOKEN")
	})

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "postgres://user:pass@localhost/morphso", cfg.DatabaseURL)
	assert.Equal(t, "https://auth.toji.homes", cfg.ZitadelIssuer)
	assert.Equal(t, "8080", cfg.Port)
}

func TestLoad_MissingRequired(t *testing.T) {
	os.Unsetenv("DATABASE_URL")
	_, err := config.Load()
	assert.ErrorContains(t, err, "DATABASE_URL")
}
```

- [ ] **Step 2: 테스트 실패 확인**

```bash
go test ./internal/config/... -v
```
Expected: `FAIL — config package not found`

- [ ] **Step 3: config.go 구현**

```go
// internal/config/config.go
package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port                string
	DatabaseURL         string
	RedisURL            string
	ZitadelIssuer       string
	ArtifactKeeperURL   string
	ArtifactKeeperToken string
}

func Load() (*Config, error) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	issuer := os.Getenv("ZITADEL_ISSUER")
	if issuer == "" {
		return nil, fmt.Errorf("ZITADEL_ISSUER is required")
	}
	akURL := os.Getenv("ARTIFACT_KEEPER_URL")
	if akURL == "" {
		return nil, fmt.Errorf("ARTIFACT_KEEPER_URL is required")
	}
	akToken := os.Getenv("ARTIFACT_KEEPER_TOKEN")
	if akToken == "" {
		return nil, fmt.Errorf("ARTIFACT_KEEPER_TOKEN is required")
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	return &Config{
		Port:                port,
		DatabaseURL:         dbURL,
		RedisURL:            os.Getenv("REDIS_URL"),
		ZitadelIssuer:       issuer,
		ArtifactKeeperURL:   akURL,
		ArtifactKeeperToken: akToken,
	}, nil
}
```

- [ ] **Step 4: 테스트 통과 확인**

```bash
go test ./internal/config/... -v
```
Expected: `PASS`

- [ ] **Step 5: 커밋**

```bash
git add internal/config/
git commit -m "feat: add config loading from environment variables"
```

---

## Task 3: 도메인 타입 정의

**Files:**
- Create: `internal/domain/package.go`
- Create: `internal/domain/install.go`
- Create: `internal/domain/user.go`

- [ ] **Step 1: package.go 작성**

```go
// internal/domain/package.go
package domain

import "time"

type PackageType string
type PackageVisibility string
type InstallStrategy string

const (
	TypePip    PackageType = "pip"
	TypeNpm    PackageType = "npm"
	TypeBinary PackageType = "binary"
	TypeMCP    PackageType = "mcp"
	TypeRecipe PackageType = "recipe"
	TypeHelm   PackageType = "helm"

	VisibilityPublic  PackageVisibility = "public"
	VisibilityPrivate PackageVisibility = "private"

	StrategyNative InstallStrategy = "native"
	StrategyDocker InstallStrategy = "docker"
	StrategyK8s    InstallStrategy = "k8s"
	StrategyHelm   InstallStrategy = "helm"
)

type Package struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	Slug                string            `json:"slug"`
	Description         string            `json:"description"`
	AuthorID            string            `json:"author_id"`
	Type                PackageType       `json:"type"`
	Visibility          PackageVisibility `json:"visibility"`
	Verified            bool              `json:"verified"`
	PriceCents          int               `json:"price_cents"`
	RecommendedStrategy InstallStrategy   `json:"recommended_strategy"`
	Downloads           int               `json:"downloads"`
	Tags                []string          `json:"tags"`
	CreatedAt           time.Time         `json:"created_at"`
	UpdatedAt           time.Time         `json:"updated_at"`
}

type PackageVersion struct {
	ID          string    `json:"id"`
	PackageID   string    `json:"package_id"`
	Version     string    `json:"version"`
	Changelog   string    `json:"changelog"`
	ArtifactURL string    `json:"artifact_url"`
	PublishedAt time.Time `json:"published_at"`
}

type OSVariant struct {
	OS            string   `json:"os"`
	Arch          string   `json:"arch"`
	Prerequisites []string `json:"prerequisites"`
	Steps         []string `json:"steps"`
}

type ResourceRequirements struct {
	MinMemoryGB float64 `json:"min_memory_gb"`
	MinDiskGB   float64 `json:"min_disk_gb"`
	NeedsGPU   bool    `json:"needs_gpu"`
}

type PackageStrategy struct {
	ID                   string               `json:"id"`
	PackageVersionID     string               `json:"package_version_id"`
	Strategy             InstallStrategy      `json:"strategy"`
	OSVariants           []OSVariant          `json:"os_variants"`
	ResourceRequirements ResourceRequirements `json:"resource_requirements"`
}
```

- [ ] **Step 2: install.go 작성**

```go
// internal/domain/install.go
package domain

type OSSpec struct {
	OS             string             `json:"os"`
	Arch           string             `json:"arch"`
	OSVersion      string             `json:"os_version"`
	MemoryTotalGB  float64            `json:"memory_total_gb"`
	MemoryFreeGB   float64            `json:"memory_free_gb"`
	DiskTotalGB    float64            `json:"disk_total_gb"`
	DiskFreeGB     float64            `json:"disk_free_gb"`
	GPU            *string            `json:"gpu"`
	InstalledTools map[string]string  `json:"installed_tools"` // tool -> version
}

type InstallStep struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

type InstallPlan struct {
	PackageSlug       string          `json:"package_slug"`
	Version           string          `json:"version"`
	Strategy          InstallStrategy `json:"strategy"`
	RecommendReason   string          `json:"recommend_reason"`
	Prerequisites     []InstallStep   `json:"prerequisites"`
	Steps             []InstallStep   `json:"steps"`
	PostInstall       []InstallStep   `json:"post_install"`
}

type RecommendRequest struct {
	PackageSlug       string          `json:"package_slug"`
	Spec              OSSpec          `json:"spec"`
	PreferredStrategy *InstallStrategy `json:"preferred_strategy"`
}
```

- [ ] **Step 3: user.go 작성**

```go
// internal/domain/user.go
package domain

import "time"

type SubscriptionTier string

const (
	TierFree SubscriptionTier = "free"
	TierPro  SubscriptionTier = "pro"
)

type UserClaims struct {
	UserID string           `json:"sub"`
	Email  string           `json:"email"`
	Tier   SubscriptionTier `json:"tier"`
}

type InstallRecord struct {
	ID          string          `json:"id"`
	UserID      string          `json:"user_id"`
	PackageSlug string          `json:"package_slug"`
	Version     string          `json:"version"`
	Strategy    InstallStrategy `json:"strategy"`
	OS          string          `json:"os"`
	Arch        string          `json:"arch"`
	InstalledAt time.Time       `json:"installed_at"`
}
```

- [ ] **Step 4: 컴파일 확인**

```bash
go build ./internal/domain/...
```
Expected: 오류 없음

- [ ] **Step 5: 커밋**

```bash
git add internal/domain/
git commit -m "feat: add core domain types (package, install, user)"
```

---

## Task 4: 데이터베이스 마이그레이션

**Files:**
- Create: `internal/db/postgres.go`
- Create: `internal/db/migrations/001_packages.sql`
- Create: `internal/db/migrations/002_install_history.sql`

- [ ] **Step 1: 001_packages.sql 작성**

```sql
-- internal/db/migrations/001_packages.sql
-- +migrate Up
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE packages (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                 TEXT NOT NULL,
    slug                 TEXT NOT NULL UNIQUE,
    description          TEXT NOT NULL DEFAULT '',
    author_id            TEXT NOT NULL,
    type                 TEXT NOT NULL,
    visibility           TEXT NOT NULL DEFAULT 'public',
    verified             BOOLEAN NOT NULL DEFAULT FALSE,
    price_cents          INTEGER NOT NULL DEFAULT 0,
    recommended_strategy TEXT NOT NULL DEFAULT 'native',
    downloads            INTEGER NOT NULL DEFAULT 0,
    tags                 TEXT[] NOT NULL DEFAULT '{}',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_packages_slug ON packages(slug);
CREATE INDEX idx_packages_author ON packages(author_id);
CREATE INDEX idx_packages_tags ON packages USING GIN(tags);

CREATE TABLE package_versions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    package_id   UUID NOT NULL REFERENCES packages(id) ON DELETE CASCADE,
    version      TEXT NOT NULL,
    changelog    TEXT NOT NULL DEFAULT '',
    artifact_url TEXT NOT NULL DEFAULT '',
    published_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(package_id, version)
);

CREATE TABLE package_strategies (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    package_version_id     UUID NOT NULL REFERENCES package_versions(id) ON DELETE CASCADE,
    strategy               TEXT NOT NULL,
    os_variants            JSONB NOT NULL DEFAULT '[]',
    resource_requirements  JSONB NOT NULL DEFAULT '{}',
    UNIQUE(package_version_id, strategy)
);

-- +migrate Down
DROP TABLE IF EXISTS package_strategies;
DROP TABLE IF EXISTS package_versions;
DROP TABLE IF EXISTS packages;
```

- [ ] **Step 2: 002_install_history.sql 작성**

```sql
-- internal/db/migrations/002_install_history.sql
-- +migrate Up
CREATE TABLE install_history (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      TEXT NOT NULL,
    package_slug TEXT NOT NULL,
    version      TEXT NOT NULL,
    strategy     TEXT NOT NULL,
    os           TEXT NOT NULL,
    arch         TEXT NOT NULL,
    installed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_install_history_user ON install_history(user_id);
CREATE INDEX idx_install_history_pkg  ON install_history(package_slug);

-- +migrate Down
DROP TABLE IF EXISTS install_history;
```

- [ ] **Step 3: postgres.go 작성**

```go
// internal/db/postgres.go
package db

import (
	"context"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return pool, nil
}

func RunMigrations(databaseURL, migrationsPath string) error {
	m, err := migrate.New("file://"+migrationsPath, databaseURL)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: 로컬 Postgres로 마이그레이션 확인 (docker-compose 사용)**

```bash
# 로컬 테스트용 Postgres 실행
docker run -d --name morphso-test-db \
  -e POSTGRES_DB=morphso \
  -e POSTGRES_USER=morphso \
  -e POSTGRES_PASSWORD=morphso \
  -p 5432:5432 postgres:16

export DATABASE_URL="postgres://morphso:morphso@localhost:5432/morphso"
make migrate-up
```
Expected: `001_packages.sql`, `002_install_history.sql` 마이그레이션 완료

- [ ] **Step 5: 커밋**

```bash
git add internal/db/
git commit -m "feat: add postgres connection pool and migrations"
```

---

## Task 5: Auth 미들웨어 (ZITADEL JWKS 검증)

**Files:**
- Create: `internal/auth/middleware.go`
- Create: `internal/auth/middleware_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

```go
// internal/auth/middleware_test.go
package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tojiuni/morphso-hub/internal/auth"
	"github.com/tojiuni/morphso-hub/internal/domain"
)

func TestRequireAuth_MissingToken(t *testing.T) {
	mw := auth.NewMiddleware("https://auth.toji.homes")
	handler := mw.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestClaimsFromContext_RoundTrip(t *testing.T) {
	claims := &domain.UserClaims{UserID: "user-1", Email: "a@b.com", Tier: domain.TierFree}
	ctx := auth.WithClaims(nil, claims) //nolint
	got := auth.ClaimsFromContext(ctx)
	assert.Equal(t, claims, got)
}
```

- [ ] **Step 2: 테스트 실패 확인**

```bash
go test ./internal/auth/... -v
```
Expected: `FAIL — auth package not found`

- [ ] **Step 3: middleware.go 구현**

```go
// internal/auth/middleware.go
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/tojiuni/morphso-hub/internal/domain"
)

type contextKey string

const claimsKey contextKey = "claims"

type Middleware struct {
	issuer  string
	jwksURL string
	cache   struct {
		sync.RWMutex
		keys    map[string]interface{}
		fetchedAt time.Time
	}
}

func NewMiddleware(issuer string) *Middleware {
	return &Middleware{
		issuer:  issuer,
		jwksURL: issuer + "/oauth/v2/keys",
	}
}

func (m *Middleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractBearer(r)
		if token == "" {
			http.Error(w, "missing authorization header", http.StatusUnauthorized)
			return
		}
		claims, err := m.validateToken(token)
		if err != nil {
			http.Error(w, "invalid token: "+err.Error(), http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithClaims(r.Context(), claims)))
	})
}

func (m *Middleware) OptionalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractBearer(r)
		if token != "" {
			if claims, err := m.validateToken(token); err == nil {
				r = r.WithContext(WithClaims(r.Context(), claims))
			}
		}
		next.ServeHTTP(w, r)
	})
}

func WithClaims(ctx context.Context, claims *domain.UserClaims) context.Context {
	return context.WithValue(ctx, claimsKey, claims)
}

func ClaimsFromContext(ctx context.Context) *domain.UserClaims {
	c, _ := ctx.Value(claimsKey).(*domain.UserClaims)
	return c
}

func extractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(h, "Bearer ")
}

func (m *Middleware) validateToken(tokenStr string) (*domain.UserClaims, error) {
	keys, err := m.fetchJWKS()
	if err != nil {
		return nil, fmt.Errorf("fetch jwks: %w", err)
	}
	var mapClaims jwt.MapClaims
	_, err = jwt.ParseWithClaims(tokenStr, &mapClaims, func(t *jwt.Token) (interface{}, error) {
		kid, _ := t.Header["kid"].(string)
		key, ok := keys[kid]
		if !ok {
			return nil, fmt.Errorf("unknown kid: %s", kid)
		}
		return key, nil
	}, jwt.WithIssuer(m.issuer), jwt.WithExpirationRequired())
	if err != nil {
		return nil, err
	}
	sub, _ := mapClaims["sub"].(string)
	email, _ := mapClaims["email"].(string)
	tier := domain.TierFree
	if t, ok := mapClaims["morphso_tier"].(string); ok && t == "pro" {
		tier = domain.TierPro
	}
	return &domain.UserClaims{UserID: sub, Email: email, Tier: tier}, nil
}

func (m *Middleware) fetchJWKS() (map[string]interface{}, error) {
	m.cache.RLock()
	if time.Since(m.cache.fetchedAt) < 5*time.Minute && m.cache.keys != nil {
		keys := m.cache.keys
		m.cache.RUnlock()
		return keys, nil
	}
	m.cache.RUnlock()

	resp, err := http.Get(m.jwksURL) //nolint:gosec
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var jwks struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, err
	}
	keys := make(map[string]interface{})
	for _, raw := range jwks.Keys {
		var header struct{ Kid string `json:"kid"` }
		if err := json.Unmarshal(raw, &header); err != nil {
			continue
		}
		key, err := jwt.ParseRSAPublicKeyFromPEM(raw)
		if err == nil {
			keys[header.Kid] = key
		}
	}
	m.cache.Lock()
	m.cache.keys = keys
	m.cache.fetchedAt = time.Now()
	m.cache.Unlock()
	return keys, nil
}
```

- [ ] **Step 4: 테스트 통과 확인**

```bash
go test ./internal/auth/... -v
```
Expected: `PASS`

- [ ] **Step 5: 커밋**

```bash
git add internal/auth/
git commit -m "feat: add ZITADEL JWKS auth middleware"
```

---

## Task 6: 패키지 레지스트리 Store

**Files:**
- Create: `internal/registry/store.go`
- Create: `internal/registry/store_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

```go
// internal/registry/store_test.go
package registry_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tojiuni/morphso-hub/internal/domain"
	"github.com/tojiuni/morphso-hub/internal/registry"
)

type mockStore struct {
	packages map[string]*domain.Package
}

func newMockStore() *mockStore {
	return &mockStore{packages: make(map[string]*domain.Package)}
}

func (m *mockStore) Create(ctx context.Context, pkg *domain.Package) error {
	m.packages[pkg.Slug] = pkg
	return nil
}

func (m *mockStore) GetBySlug(ctx context.Context, slug string) (*domain.Package, error) {
	p, ok := m.packages[slug]
	if !ok {
		return nil, registry.ErrNotFound
	}
	return p, nil
}

func (m *mockStore) Search(ctx context.Context, query string, limit, offset int) ([]*domain.Package, error) {
	var results []*domain.Package
	for _, p := range m.packages {
		results = append(results, p)
	}
	return results, nil
}

func TestStore_CreateAndGet(t *testing.T) {
	store := newMockStore()
	pkg := &domain.Package{
		Slug:        "gopedia",
		Name:        "gopedia",
		Description: "gopedia package",
		AuthorID:    "user-1",
		Type:        domain.TypeMCP,
		Visibility:  domain.VisibilityPublic,
	}
	err := store.Create(context.Background(), pkg)
	require.NoError(t, err)

	got, err := store.GetBySlug(context.Background(), "gopedia")
	require.NoError(t, err)
	assert.Equal(t, "gopedia", got.Slug)
}

func TestStore_GetBySlug_NotFound(t *testing.T) {
	store := newMockStore()
	_, err := store.GetBySlug(context.Background(), "nonexistent")
	assert.ErrorIs(t, err, registry.ErrNotFound)
}
```

- [ ] **Step 2: 테스트 실패 확인**

```bash
go test ./internal/registry/... -v
```
Expected: `FAIL — registry package not found`

- [ ] **Step 3: store.go 구현**

```go
// internal/registry/store.go
package registry

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tojiuni/morphso-hub/internal/domain"
)

var ErrNotFound = errors.New("package not found")

type Store interface {
	Create(ctx context.Context, pkg *domain.Package) error
	GetBySlug(ctx context.Context, slug string) (*domain.Package, error)
	Search(ctx context.Context, query string, limit, offset int) ([]*domain.Package, error)
}

type pgStore struct {
	pool *pgxpool.Pool
}

func NewPGStore(pool *pgxpool.Pool) Store {
	return &pgStore{pool: pool}
}

func (s *pgStore) Create(ctx context.Context, pkg *domain.Package) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO packages
			(name, slug, description, author_id, type, visibility, verified, price_cents, recommended_strategy, tags)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		pkg.Name, pkg.Slug, pkg.Description, pkg.AuthorID, pkg.Type,
		pkg.Visibility, pkg.Verified, pkg.PriceCents, pkg.RecommendedStrategy, pkg.Tags,
	)
	return err
}

func (s *pgStore) GetBySlug(ctx context.Context, slug string) (*domain.Package, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, name, slug, description, author_id, type, visibility,
		       verified, price_cents, recommended_strategy, downloads, tags, created_at, updated_at
		FROM packages WHERE slug = $1`, slug)

	var pkg domain.Package
	err := row.Scan(
		&pkg.ID, &pkg.Name, &pkg.Slug, &pkg.Description, &pkg.AuthorID,
		&pkg.Type, &pkg.Visibility, &pkg.Verified, &pkg.PriceCents,
		&pkg.RecommendedStrategy, &pkg.Downloads, &pkg.Tags, &pkg.CreatedAt, &pkg.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan package: %w", err)
	}
	return &pkg, nil
}

func (s *pgStore) Search(ctx context.Context, query string, limit, offset int) ([]*domain.Package, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, slug, description, author_id, type, visibility,
		       verified, price_cents, recommended_strategy, downloads, tags, created_at, updated_at
		FROM packages
		WHERE visibility = 'public'
		  AND ($1 = '' OR name ILIKE '%' || $1 || '%' OR description ILIKE '%' || $1 || '%')
		ORDER BY downloads DESC
		LIMIT $2 OFFSET $3`, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("search packages: %w", err)
	}
	defer rows.Close()

	var pkgs []*domain.Package
	for rows.Next() {
		var pkg domain.Package
		if err := rows.Scan(
			&pkg.ID, &pkg.Name, &pkg.Slug, &pkg.Description, &pkg.AuthorID,
			&pkg.Type, &pkg.Visibility, &pkg.Verified, &pkg.PriceCents,
			&pkg.RecommendedStrategy, &pkg.Downloads, &pkg.Tags, &pkg.CreatedAt, &pkg.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		pkgs = append(pkgs, &pkg)
	}
	return pkgs, nil
}
```

- [ ] **Step 4: 테스트 통과 확인**

```bash
go test ./internal/registry/... -v
```
Expected: `PASS`

- [ ] **Step 5: 커밋**

```bash
git add internal/registry/
git commit -m "feat: add package registry store interface and pg implementation"
```

---

## Task 7: 추천 엔진 (rule-based)

**Files:**
- Create: `internal/recommend/engine.go`
- Create: `internal/recommend/engine_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

```go
// internal/recommend/engine_test.go
package recommend_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tojiuni/morphso-hub/internal/domain"
	"github.com/tojiuni/morphso-hub/internal/recommend"
)

func spec(tools map[string]string, memFreeGB float64) domain.OSSpec {
	return domain.OSSpec{
		OS:            "linux",
		Arch:          "amd64",
		MemoryFreeGB:  memFreeGB,
		DiskFreeGB:    100,
		InstalledTools: tools,
	}
}

func TestEngine_PrefersK8sWhenKubectlAndMemory(t *testing.T) {
	e := recommend.NewEngine()
	spec := spec(map[string]string{"kubectl": "1.35.0", "helm": "3.15.0"}, 8.0)
	result := e.Recommend(spec, nil)
	assert.Equal(t, domain.StrategyK8s, result.Strategy)
	assert.NotEmpty(t, result.Reason)
}

func TestEngine_PrefersDockerWhenNoKubectl(t *testing.T) {
	e := recommend.NewEngine()
	spec := spec(map[string]string{"docker": "27.0.0"}, 4.0)
	result := e.Recommend(spec, nil)
	assert.Equal(t, domain.StrategyDocker, result.Strategy)
}

func TestEngine_FallsBackToNative(t *testing.T) {
	e := recommend.NewEngine()
	spec := spec(map[string]string{}, 1.0)
	result := e.Recommend(spec, nil)
	assert.Equal(t, domain.StrategyNative, result.Strategy)
}

func TestEngine_HonorsPreferredStrategy(t *testing.T) {
	e := recommend.NewEngine()
	s := spec(map[string]string{"kubectl": "1.35.0"}, 8.0)
	preferred := domain.StrategyDocker
	result := e.Recommend(s, &preferred)
	assert.Equal(t, domain.StrategyDocker, result.Strategy)
}
```

- [ ] **Step 2: 테스트 실패 확인**

```bash
go test ./internal/recommend/... -v
```
Expected: `FAIL — recommend package not found`

- [ ] **Step 3: engine.go 구현**

```go
// internal/recommend/engine.go
package recommend

import (
	"fmt"

	"github.com/tojiuni/morphso-hub/internal/domain"
)

type Result struct {
	Strategy domain.InstallStrategy
	Reason   string
}

type Engine struct{}

func NewEngine() *Engine { return &Engine{} }

func (e *Engine) Recommend(spec domain.OSSpec, preferred *domain.InstallStrategy) Result {
	if preferred != nil {
		return Result{
			Strategy: *preferred,
			Reason:   fmt.Sprintf("사용자가 %s 전략을 직접 선택했습니다", *preferred),
		}
	}
	_, hasKubectl := spec.InstalledTools["kubectl"]
	_, hasHelm := spec.InstalledTools["helm"]
	_, hasDocker := spec.InstalledTools["docker"]

	if hasKubectl && hasHelm && spec.MemoryFreeGB >= 4 {
		return Result{
			Strategy: domain.StrategyK8s,
			Reason:   fmt.Sprintf("kubectl/helm 설치됨, 여유 메모리 %.1fGB — K8s 배포 권장", spec.MemoryFreeGB),
		}
	}
	if hasDocker && spec.MemoryFreeGB >= 2 {
		return Result{
			Strategy: domain.StrategyDocker,
			Reason:   fmt.Sprintf("Docker 설치됨, 여유 메모리 %.1fGB — Docker 실행 권장", spec.MemoryFreeGB),
		}
	}
	return Result{
		Strategy: domain.StrategyNative,
		Reason:   "로컬 네이티브 설치 (pip/npm/binary)",
	}
}
```

- [ ] **Step 4: 테스트 통과 확인**

```bash
go test ./internal/recommend/... -v
```
Expected: `PASS`

- [ ] **Step 5: 커밋**

```bash
git add internal/recommend/
git commit -m "feat: add rule-based install strategy recommendation engine"
```

---

## Task 8: HTTP 라우터 & 핸들러

**Files:**
- Create: `internal/api/router.go`
- Create: `internal/api/handlers/health.go`
- Create: `internal/api/handlers/packages.go`
- Create: `internal/api/handlers/recommend.go`
- Create: `internal/api/handlers/installs.go`
- Create: `internal/api/handlers/packages_test.go`
- Create: `internal/api/handlers/recommend_test.go`

- [ ] **Step 1: health.go 작성**

```go
// internal/api/handlers/health.go
package handlers

import (
	"encoding/json"
	"net/http"
)

func Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
```

- [ ] **Step 2: packages.go 작성**

```go
// internal/api/handlers/packages.go
package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/tojiuni/morphso-hub/internal/domain"
	"github.com/tojiuni/morphso-hub/internal/auth"
	"github.com/tojiuni/morphso-hub/internal/registry"
)

type PackageHandler struct {
	store registry.Store
}

func NewPackageHandler(store registry.Store) *PackageHandler {
	return &PackageHandler{store: store}
}

func (h *PackageHandler) GetBySlug(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	pkg, err := h.store.GetBySlug(r.Context(), slug)
	if errors.Is(err, registry.ErrNotFound) {
		http.Error(w, "package not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pkg)
}

func (h *PackageHandler) Search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	pkgs, err := h.store.Search(r.Context(), q, 20, 0)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pkgs)
}

func (h *PackageHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var pkg domain.Package
	if err := json.NewDecoder(r.Body).Decode(&pkg); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	pkg.AuthorID = claims.UserID
	pkg.Visibility = domain.VisibilityPublic
	if err := h.store.Create(r.Context(), &pkg); err != nil {
		http.Error(w, "failed to create package", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(pkg)
}
```

- [ ] **Step 3: recommend.go 작성**

```go
// internal/api/handlers/recommend.go
package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/tojiuni/morphso-hub/internal/domain"
	"github.com/tojiuni/morphso-hub/internal/recommend"
)

type RecommendHandler struct {
	engine *recommend.Engine
}

func NewRecommendHandler(engine *recommend.Engine) *RecommendHandler {
	return &RecommendHandler{engine: engine}
}

func (h *RecommendHandler) Recommend(w http.ResponseWriter, r *http.Request) {
	var req domain.RecommendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	result := h.engine.Recommend(req.Spec, req.PreferredStrategy)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
```

- [ ] **Step 4: installs.go 작성**

```go
// internal/api/handlers/installs.go
package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tojiuni/morphso-hub/internal/auth"
	"github.com/tojiuni/morphso-hub/internal/domain"
)

type InstallHandler struct {
	pool *pgxpool.Pool
}

func NewInstallHandler(pool *pgxpool.Pool) *InstallHandler {
	return &InstallHandler{pool: pool}
}

func (h *InstallHandler) RecordInstall(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		w.WriteHeader(http.StatusNoContent) // 비로그인 무시
		return
	}
	var rec domain.InstallRecord
	if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	rec.UserID = claims.UserID
	if err := h.insertRecord(r.Context(), &rec); err != nil {
		http.Error(w, "failed to record", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

func (h *InstallHandler) insertRecord(ctx context.Context, rec *domain.InstallRecord) error {
	_, err := h.pool.Exec(ctx, `
		INSERT INTO install_history (user_id, package_slug, version, strategy, os, arch)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		rec.UserID, rec.PackageSlug, rec.Version, rec.Strategy, rec.OS, rec.Arch,
	)
	return err
}

func (h *InstallHandler) ListMyInstalls(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT id, user_id, package_slug, version, strategy, os, arch, installed_at
		FROM install_history WHERE user_id = $1
		ORDER BY installed_at DESC LIMIT 50`, claims.UserID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var records []domain.InstallRecord
	for rows.Next() {
		var rec domain.InstallRecord
		if err := rows.Scan(
			&rec.ID, &rec.UserID, &rec.PackageSlug, &rec.Version,
			&rec.Strategy, &rec.OS, &rec.Arch, &rec.InstalledAt,
		); err != nil {
			http.Error(w, "scan error", http.StatusInternalServerError)
			return
		}
		records = append(records, rec)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(records)
}
```

- [ ] **Step 5: router.go 작성**

```go
// internal/api/router.go
package api

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tojiuni/morphso-hub/internal/api/handlers"
	"github.com/tojiuni/morphso-hub/internal/auth"
	"github.com/tojiuni/morphso-hub/internal/recommend"
	"github.com/tojiuni/morphso-hub/internal/registry"
)

func NewRouter(
	pool *pgxpool.Pool,
	store registry.Store,
	authMW *auth.Middleware,
	engine *recommend.Engine,
) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	pkgHandler := handlers.NewPackageHandler(store)
	recHandler := handlers.NewRecommendHandler(engine)
	instHandler := handlers.NewInstallHandler(pool)

	r.Get("/health", handlers.Health)

	r.Group(func(r chi.Router) {
		r.Use(authMW.OptionalAuth)
		r.Get("/packages", pkgHandler.Search)
		r.Get("/packages/{slug}", pkgHandler.GetBySlug)
	})

	r.Group(func(r chi.Router) {
		r.Use(authMW.RequireAuth)
		r.Post("/packages", pkgHandler.Create)
		r.Post("/recommend", recHandler.Recommend)
		r.Post("/installs", instHandler.RecordInstall)
		r.Get("/users/me/installs", instHandler.ListMyInstalls)
	})

	return r
}
```

- [ ] **Step 6: recommend 핸들러 테스트 작성 & 실행**

```go
// internal/api/handlers/recommend_test.go
package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tojiuni/morphso-hub/internal/api/handlers"
	"github.com/tojiuni/morphso-hub/internal/domain"
	"github.com/tojiuni/morphso-hub/internal/recommend"
)

func TestRecommendHandler_K8sRecommendation(t *testing.T) {
	h := handlers.NewRecommendHandler(recommend.NewEngine())

	req := domain.RecommendRequest{
		PackageSlug: "gopedia",
		Spec: domain.OSSpec{
			OS:            "linux",
			Arch:          "amd64",
			MemoryFreeGB:  8.0,
			InstalledTools: map[string]string{"kubectl": "1.35.0", "helm": "3.15.0"},
		},
	}
	body, _ := json.Marshal(req)
	r := httptest.NewRequest(http.MethodPost, "/recommend", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.Recommend(rr, r)

	require.Equal(t, http.StatusOK, rr.Code)
	var result recommend.Result
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&result))
	assert.Equal(t, domain.StrategyK8s, result.Strategy)
}
```

```bash
go test ./internal/api/... -v
```
Expected: `PASS`

- [ ] **Step 7: main.go 완성 (의존성 조립)**

```go
// cmd/server/main.go
package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/tojiuni/morphso-hub/internal/api"
	"github.com/tojiuni/morphso-hub/internal/auth"
	"github.com/tojiuni/morphso-hub/internal/config"
	"github.com/tojiuni/morphso-hub/internal/db"
	"github.com/tojiuni/morphso-hub/internal/recommend"
	"github.com/tojiuni/morphso-hub/internal/registry"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if err := db.RunMigrations(cfg.DatabaseURL, "internal/db/migrations"); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	pool, err := db.NewPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	store := registry.NewPGStore(pool)
	authMW := auth.NewMiddleware(cfg.ZitadelIssuer)
	engine := recommend.NewEngine()

	router := api.NewRouter(pool, store, authMW, engine)

	log.Printf("morphso-hub listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, router); err != nil {
		log.Fatalf("server: %v", err)
	}
}
```

- [ ] **Step 8: 전체 빌드 & 테스트**

```bash
make test
make build
```
Expected: 모든 테스트 PASS, 바이너리 생성

- [ ] **Step 9: 커밋**

```bash
git add internal/api/ cmd/
git commit -m "feat: add HTTP router and all core API handlers"
```

---

## Task 9: artifact-keeper 클라이언트

**Files:**
- Create: `internal/registry/artifact.go`
- Create: `internal/registry/artifact_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

```go
// internal/registry/artifact_test.go
package registry_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tojiuni/morphso-hub/internal/registry"
)

func TestArtifactClient_Upload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, "/api/v1/artifacts/morphso/gopedia/1.0.0/gopedia.tar.gz", r.URL.Path)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"url":"https://artifacts.example.com/morphso/gopedia/1.0.0/gopedia.tar.gz"}`))
	}))
	defer srv.Close()

	client := registry.NewArtifactClient(srv.URL, "test-token")
	url, err := client.Upload(t.Context(), "gopedia", "1.0.0", "gopedia.tar.gz", strings.NewReader("file-content"))
	require.NoError(t, err)
	assert.Contains(t, url, "gopedia.tar.gz")
}
```

- [ ] **Step 2: 테스트 실패 확인**

```bash
go test ./internal/registry/... -run TestArtifactClient -v
```
Expected: `FAIL`

- [ ] **Step 3: artifact.go 구현**

```go
// internal/registry/artifact.go
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type ArtifactClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewArtifactClient(baseURL, token string) *ArtifactClient {
	return &ArtifactClient{baseURL: baseURL, token: token, http: &http.Client{}}
}

func (c *ArtifactClient) Upload(ctx context.Context, pkg, version, filename string, body io.Reader) (string, error) {
	path := fmt.Sprintf("/api/v1/artifacts/morphso/%s/%s/%s", pkg, version, filename)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+path, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload artifact: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("artifact-keeper returned %d", resp.StatusCode)
	}
	var result struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	return result.URL, nil
}

func (c *ArtifactClient) DownloadURL(pkg, version, filename string) string {
	return fmt.Sprintf("%s/morphso/%s/%s/%s", c.baseURL, pkg, version, filename)
}
```

- [ ] **Step 4: 테스트 통과 확인**

```bash
go test ./internal/registry/... -v
```
Expected: `PASS`

- [ ] **Step 5: 커밋**

```bash
git add internal/registry/artifact.go internal/registry/artifact_test.go
git commit -m "feat: add artifact-keeper HTTP client for file upload/download"
```

---

## Task 10: Dockerfile & 로컬 개발 환경

**Files:**
- Create: `Dockerfile`
- Create: `docker-compose.yml`
- Create: `.env.example`

- [ ] **Step 1: Dockerfile 작성**

```dockerfile
# Dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /morphso-hub ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /morphso-hub /morphso-hub
COPY --from=builder /app/internal/db/migrations /migrations
EXPOSE 8080
ENTRYPOINT ["/morphso-hub"]
```

- [ ] **Step 2: docker-compose.yml 작성 (로컬 개발용)**

```yaml
# docker-compose.yml
services:
  db:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: morphso
      POSTGRES_USER: morphso
      POSTGRES_PASSWORD: morphso
    ports:
      - "5432:5432"
    volumes:
      - pg_data:/var/lib/postgresql/data

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"

  morphso-hub:
    build: .
    ports:
      - "8080:8080"
    env_file: .env
    depends_on:
      - db
      - redis

volumes:
  pg_data:
```

- [ ] **Step 3: .env.example 작성**

```bash
# .env.example
PORT=8080
DATABASE_URL=postgres://morphso:morphso@db:5432/morphso
REDIS_URL=redis://redis:6379
ZITADEL_ISSUER=https://auth.toji.homes
ARTIFACT_KEEPER_URL=https://artifacts.toji.homes
ARTIFACT_KEEPER_TOKEN=<vault에서 주입>
```

- [ ] **Step 4: 도커 빌드 확인**

```bash
docker build -t morphso-hub:dev .
```
Expected: 이미지 빌드 성공

- [ ] **Step 5: docker-compose로 전체 스택 실행 확인**

```bash
cp .env.example .env  # 로컬값 채우기
docker compose up -d
curl http://localhost:8080/health
```
Expected: `{"status":"ok"}`

- [ ] **Step 6: 커밋**

```bash
git add Dockerfile docker-compose.yml .env.example
git commit -m "chore: add Dockerfile and docker-compose for local development"
```

---

## Task 11: 최종 검증

- [ ] **Step 1: 전체 테스트 실행**

```bash
make test
```
Expected: 모든 테스트 PASS, race condition 없음

- [ ] **Step 2: 주요 API 엔드포인트 smoke test**

```bash
# 헬스체크
curl http://localhost:8080/health

# 패키지 검색 (비인증)
curl "http://localhost:8080/packages?q=gopedia"

# 추천 (인증 필요)
curl -X POST http://localhost:8080/recommend \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"package_slug":"gopedia","spec":{"os":"darwin","arch":"arm64","memory_free_gb":18,"installed_tools":{"kubectl":"1.35.0","helm":"3.15.0"}}}'
```
Expected: 각각 200 OK, 빈 배열 `[]`, 추천 결과 JSON

- [ ] **Step 3: 최종 커밋**

```bash
git add -A
git commit -m "feat: morphso-hub core MVP complete"
```

---

## 다음 계획

- **Plan 2**: morphso-hub Billing — Stripe 구독 + 마켓플레이스 결제 + Stripe Connect 정산
- **Plan 3**: morphso CLI — Go 단일 바이너리, OS 스펙 수집, AI 추천 연동, install strategy 실행
- **Plan 4**: neunexus K8s 배포 — morphso ns, Vault 시크릿, Traefik IngressRoute, ArgoCD
