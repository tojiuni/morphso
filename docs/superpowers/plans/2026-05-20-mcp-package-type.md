# MCP Package Type (1st-class) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable `moso install gopedia-mcp` end-to-end: hub stores structured MCP metadata, CLI auto-registers the server to detected MCP clients (Claude Code / Cursor / Gemini CLI) with prompted env vars.

**Architecture:** hub gets a new JSONB `mcp_metadata` column + validation rules; CLI gets a new `internal/mcpclient/` package implementing a per-client `Register`/`Unregister` interface (each handling its own JSON path: `mcpServers` for Cursor, `mcp.servers` for Gemini, `claude mcp add-json` subprocess for Claude Code). `cmd/install.go` hooks `runMCPRegistration` after the install script succeeds.

**Tech Stack:** Go 1.26, PostgreSQL + pgx/v5, golang-migrate, cobra CLI, stretchr/testify.

**Spec:** `docs/superpowers/specs/2026-05-20-gopedia-mcp-install-design.md` (read first).

**Worktrees:**
- morphso (CLI): `/Users/dong-hoshin/Documents/dev/morphso-wt-mcp-type` — branch `feat/mcp-package-type`
- morphso-hub: `/Users/dong-hoshin/Documents/dev/morphso-hub-wt-mcp-type` — branch `feat/mcp-package-type`

**Execution order:** Phase A (hub) → Phase B (CLI) → Phase C (data + E2E). hub PR merges/deploys before CLI release.

---

## Phase A — morphso-hub

Working directory for all Phase A tasks:
```
cd /Users/dong-hoshin/Documents/dev/morphso-hub-wt-mcp-type
```

### Task A1: Add migration for `mcp_metadata` column

**Files:**
- Create: `internal/db/migrations/010_mcp_metadata.up.sql`
- Create: `internal/db/migrations/010_mcp_metadata.down.sql`

- [ ] **Step 1: Create up migration**

Write `internal/db/migrations/010_mcp_metadata.up.sql`:
```sql
ALTER TABLE packages ADD COLUMN mcp_metadata JSONB;
```

- [ ] **Step 2: Create down migration**

Write `internal/db/migrations/010_mcp_metadata.down.sql`:
```sql
ALTER TABLE packages DROP COLUMN mcp_metadata;
```

- [ ] **Step 3: Verify migration files lint**

Run: `ls internal/db/migrations/010_mcp_metadata.*`
Expected: both files exist.

- [ ] **Step 4: Commit**
```bash
git add internal/db/migrations/010_mcp_metadata.up.sql internal/db/migrations/010_mcp_metadata.down.sql
git commit -m "feat: add mcp_metadata jsonb column migration"
```

---

### Task A2: Add MCPMetadata domain types

**Files:**
- Modify: `internal/domain/package.go`
- Test: `internal/domain/package_test.go` (create if missing)

- [ ] **Step 1: Write failing test for MCPMetadata JSON round-trip**

Create or extend `internal/domain/package_test.go`:
```go
package domain

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPMetadata_JSONRoundTrip(t *testing.T) {
	original := MCPMetadata{
		ServerName: "gopedia",
		Transport:  "stdio",
		Native: &MCPCommandSpec{
			Command: "gopedia-mcp-server",
			Args:    []string{},
		},
		Docker: &MCPCommandSpec{
			Image: "artifacts.toji.homes/gopedia-mcp",
			Args:  []string{"run", "-i", "--rm", "-e", "GOPEDIA_HOST_DOMAIN", "{{image}}"},
		},
		EnvSchema: []MCPEnvSpec{
			{Name: "GOPEDIA_HOST_DOMAIN", Prompt: "host", Required: true, Default: "127.0.0.1:18787"},
		},
	}
	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded MCPMetadata
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, original.ServerName, decoded.ServerName)
	assert.Equal(t, original.Transport, decoded.Transport)
	assert.Equal(t, original.Native.Command, decoded.Native.Command)
	assert.Equal(t, original.Docker.Image, decoded.Docker.Image)
	assert.Equal(t, original.EnvSchema[0].Name, decoded.EnvSchema[0].Name)
}

func TestMCPMetadata_JSONBValue(t *testing.T) {
	m := MCPMetadata{ServerName: "x", Transport: "stdio"}
	v, err := m.Value()
	require.NoError(t, err)
	b, ok := v.([]byte)
	require.True(t, ok)
	assert.Contains(t, string(b), `"server_name":"x"`)
}

func TestMCPMetadata_JSONBScan(t *testing.T) {
	var m MCPMetadata
	err := m.Scan([]byte(`{"server_name":"x","transport":"stdio"}`))
	require.NoError(t, err)
	assert.Equal(t, "x", m.ServerName)

	var n MCPMetadata
	err = n.Scan(nil)
	require.NoError(t, err)
	assert.Empty(t, n.ServerName)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/ -run TestMCPMetadata -v`
Expected: FAIL — undefined: MCPMetadata.

- [ ] **Step 3: Add MCPMetadata types and Scan/Value to package.go**

Append to `internal/domain/package.go`:
```go
import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

type MCPMetadata struct {
	ServerName        string                  `json:"server_name"`
	Transport         string                  `json:"transport,omitempty"`
	Native            *MCPCommandSpec         `json:"native,omitempty"`
	Docker            *MCPCommandSpec         `json:"docker,omitempty"`
	PlatformOverrides map[string]MCPOverride  `json:"platform_overrides,omitempty"`
	EnvSchema         []MCPEnvSpec            `json:"env_schema,omitempty"`
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

func (m MCPMetadata) Value() (driver.Value, error) {
	return json.Marshal(m)
}

func (m *MCPMetadata) Scan(src any) error {
	if src == nil {
		*m = MCPMetadata{}
		return nil
	}
	b, ok := src.([]byte)
	if !ok {
		return fmt.Errorf("MCPMetadata.Scan: expected []byte, got %T", src)
	}
	return json.Unmarshal(b, m)
}
```

Add to existing `Package` struct (after `Tags`):
```go
	MCPMetadata *MCPMetadata `json:"mcp_metadata,omitempty"`
```

(Note: the `import` block needs to be merged with existing imports — there's already `time` import.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/domain/ -run TestMCPMetadata -v`
Expected: PASS.

- [ ] **Step 5: Commit**
```bash
git add internal/domain/package.go internal/domain/package_test.go
git commit -m "feat: add MCPMetadata domain type with JSONB scan/value"
```

---

### Task A3: Update registry store for mcp_metadata column

**Files:**
- Modify: `internal/registry/store.go` (Create, GetBySlug, Search)
- Test: `internal/registry/store_test.go`

- [ ] **Step 1: Write failing test for round-trip persistence**

Add to `internal/registry/store_test.go`:
```go
func TestStore_MCPMetadataRoundTrip(t *testing.T) {
	pool := testPool(t) // existing test helper
	store := NewPGStore(pool)
	ctx := context.Background()

	pkg := &domain.Package{
		Name: "GoPedia MCP", Slug: "gopedia-mcp",
		Type: domain.TypeMCP, Visibility: domain.VisibilityPublic,
		Tags: []string{"mcp"},
		MCPMetadata: &domain.MCPMetadata{
			ServerName: "gopedia",
			Transport:  "stdio",
			Native:     &domain.MCPCommandSpec{Command: "gopedia-mcp-server"},
			EnvSchema:  []domain.MCPEnvSpec{{Name: "X", Required: true}},
		},
	}
	require.NoError(t, store.Create(ctx, pkg))

	got, err := store.GetBySlug(ctx, "gopedia-mcp")
	require.NoError(t, err)
	require.NotNil(t, got.MCPMetadata)
	assert.Equal(t, "gopedia", got.MCPMetadata.ServerName)
	assert.Equal(t, "stdio", got.MCPMetadata.Transport)
	assert.Equal(t, "gopedia-mcp-server", got.MCPMetadata.Native.Command)
	assert.Equal(t, "X", got.MCPMetadata.EnvSchema[0].Name)
}

func TestStore_NonMCPNullMetadata(t *testing.T) {
	pool := testPool(t)
	store := NewPGStore(pool)
	ctx := context.Background()

	pkg := &domain.Package{
		Name: "foo", Slug: "foo-pkg",
		Type: domain.TypeNpm, Visibility: domain.VisibilityPublic, Tags: []string{},
	}
	require.NoError(t, store.Create(ctx, pkg))

	got, err := store.GetBySlug(ctx, "foo-pkg")
	require.NoError(t, err)
	assert.Nil(t, got.MCPMetadata)
}
```

If `testPool` helper doesn't exist, check existing test file for the standard pgx test fixture pattern; reuse it.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/registry/ -run TestStore_MCP -v`
Expected: FAIL — column does not exist (or panic on scan).

- [ ] **Step 3: Update Create query**

In `internal/registry/store.go`, modify the Create method INSERT:
```go
func (s *pgStore) Create(ctx context.Context, pkg *domain.Package) error {
	var mcpMeta any
	if pkg.MCPMetadata != nil {
		mcpMeta = pkg.MCPMetadata // Value() implements driver.Valuer
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO packages
			(name, slug, description, author_id, type, visibility, verified, price_cents, recommended_strategy, tags, mcp_metadata)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		pkg.Name, pkg.Slug, pkg.Description, pkg.AuthorID, pkg.Type,
		pkg.Visibility, pkg.Verified, pkg.PriceCents, pkg.RecommendedStrategy, pkg.Tags,
		mcpMeta,
	)
	return err
}
```

- [ ] **Step 4: Update GetBySlug query and scan**

Modify GetBySlug:
```go
func (s *pgStore) GetBySlug(ctx context.Context, slug string) (*domain.Package, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, name, slug, description, author_id, type, visibility,
		       verified, price_cents, recommended_strategy, downloads, tags,
		       mcp_metadata, created_at, updated_at
		FROM packages WHERE slug = $1`, slug)

	var pkg domain.Package
	var mcpMeta []byte
	err := row.Scan(
		&pkg.ID, &pkg.Name, &pkg.Slug, &pkg.Description, &pkg.AuthorID,
		&pkg.Type, &pkg.Visibility, &pkg.Verified, &pkg.PriceCents,
		&pkg.RecommendedStrategy, &pkg.Downloads, &pkg.Tags,
		&mcpMeta,
		&pkg.CreatedAt, &pkg.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan package: %w", err)
	}
	if len(mcpMeta) > 0 {
		var m domain.MCPMetadata
		if err := json.Unmarshal(mcpMeta, &m); err == nil {
			pkg.MCPMetadata = &m
		}
	}
	return &pkg, nil
}
```

(Add `"encoding/json"` to imports.)

- [ ] **Step 5: Update Search query**

Apply the same column addition + scan pattern to the Search method's loop. Use the exact same `mcpMeta []byte` + post-scan unmarshal pattern.

- [ ] **Step 6: Run all registry tests**

Run: `go test ./internal/registry/ -v`
Expected: PASS (new tests + existing tests).

- [ ] **Step 7: Commit**
```bash
git add internal/registry/store.go internal/registry/store_test.go
git commit -m "feat: persist mcp_metadata in package store"
```

---

### Task A4: API handlers — accept/validate/return mcp_metadata

**Files:**
- Modify: `internal/api/handlers/packages.go`
- Create: `internal/api/handlers/mcp_validate.go`
- Create: `internal/api/handlers/mcp_validate_test.go`
- Modify: `internal/api/handlers/packages_test.go`

- [ ] **Step 1: Write failing test for validation helper**

Create `internal/api/handlers/mcp_validate_test.go`:
```go
package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tojiuni/morphso-hub/internal/domain"
)

func TestValidateMCPMetadata_OK(t *testing.T) {
	m := &domain.MCPMetadata{
		ServerName: "gopedia",
		Transport:  "stdio",
		Native:     &domain.MCPCommandSpec{Command: "x", Args: []string{"--flag", "{{slug}}"}},
		EnvSchema:  []domain.MCPEnvSpec{{Name: "GOPEDIA_HOST_DOMAIN", Required: true}},
	}
	assert.NoError(t, ValidateMCPMetadata(m))
}

func TestValidateMCPMetadata_MissingServerName(t *testing.T) {
	m := &domain.MCPMetadata{Native: &domain.MCPCommandSpec{Command: "x"}}
	err := ValidateMCPMetadata(m)
	assert.ErrorContains(t, err, "server_name")
}

func TestValidateMCPMetadata_InvalidServerName(t *testing.T) {
	m := &domain.MCPMetadata{ServerName: "Bad Name!", Native: &domain.MCPCommandSpec{Command: "x"}}
	err := ValidateMCPMetadata(m)
	assert.ErrorContains(t, err, "server_name")
}

func TestValidateMCPMetadata_NoNativeOrDocker(t *testing.T) {
	m := &domain.MCPMetadata{ServerName: "ok"}
	err := ValidateMCPMetadata(m)
	assert.ErrorContains(t, err, "native or docker")
}

func TestValidateMCPMetadata_InvalidEnvName(t *testing.T) {
	m := &domain.MCPMetadata{
		ServerName: "ok",
		Native:     &domain.MCPCommandSpec{Command: "x"},
		EnvSchema:  []domain.MCPEnvSpec{{Name: "lowercase_bad", Required: true}},
	}
	err := ValidateMCPMetadata(m)
	assert.ErrorContains(t, err, "env name")
}

func TestValidateMCPMetadata_UnknownToken(t *testing.T) {
	m := &domain.MCPMetadata{
		ServerName: "ok",
		Native:     &domain.MCPCommandSpec{Command: "x", Args: []string{"{{mystery}}"}},
	}
	err := ValidateMCPMetadata(m)
	assert.ErrorContains(t, err, "unknown token")
}

func TestValidateMCPMetadata_EnvTokenInArgsForbidden(t *testing.T) {
	m := &domain.MCPMetadata{
		ServerName: "ok",
		Docker:     &domain.MCPCommandSpec{Image: "img", Args: []string{"-e", "FOO={{env:FOO}}"}},
		EnvSchema:  []domain.MCPEnvSpec{{Name: "FOO", Required: true}},
	}
	err := ValidateMCPMetadata(m)
	assert.ErrorContains(t, err, "env value in args")
}

func TestValidateMCPMetadata_UnsupportedTransport(t *testing.T) {
	m := &domain.MCPMetadata{
		ServerName: "ok",
		Transport:  "carrier-pigeon",
		Native:     &domain.MCPCommandSpec{Command: "x"},
	}
	err := ValidateMCPMetadata(m)
	assert.ErrorContains(t, err, "transport")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/handlers/ -run TestValidateMCPMetadata -v`
Expected: FAIL — undefined: ValidateMCPMetadata.

- [ ] **Step 3: Implement validator**

Create `internal/api/handlers/mcp_validate.go`:
```go
package handlers

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/tojiuni/morphso-hub/internal/domain"
)

var (
	serverNameRe  = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	envNameRe     = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	tokenRe       = regexp.MustCompile(`\{\{([a-z_:A-Z0-9-]+)\}\}`)
	allowedTokens = map[string]bool{"image": true, "slug": true, "version": true}
)

func ValidateMCPMetadata(m *domain.MCPMetadata) error {
	if m == nil {
		return fmt.Errorf("mcp_metadata required for type=mcp")
	}
	if !serverNameRe.MatchString(m.ServerName) {
		return fmt.Errorf("server_name must match %s", serverNameRe.String())
	}
	switch m.Transport {
	case "", "stdio", "http", "sse":
	default:
		return fmt.Errorf("transport %q not supported", m.Transport)
	}
	if m.Native == nil && m.Docker == nil {
		return fmt.Errorf("native or docker spec required (at least one)")
	}
	envSet := map[string]bool{}
	for _, e := range m.EnvSchema {
		if !envNameRe.MatchString(e.Name) {
			return fmt.Errorf("env name %q must match %s", e.Name, envNameRe.String())
		}
		envSet[e.Name] = true
	}
	for _, spec := range []*domain.MCPCommandSpec{m.Native, m.Docker} {
		if spec == nil {
			continue
		}
		for _, arg := range spec.Args {
			if err := validateTokens(arg, envSet); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateTokens enforces token namespace + forbids {{env:VAR}} inside larger
// strings (e.g. `FOO={{env:FOO}}`) to prevent leaking values via process args.
func validateTokens(arg string, envSet map[string]bool) error {
	for _, m := range tokenRe.FindAllStringSubmatch(arg, -1) {
		tok := m[1]
		if strings.HasPrefix(tok, "env:") {
			name := strings.TrimPrefix(tok, "env:")
			if !envSet[name] {
				return fmt.Errorf("env token references undeclared %q", name)
			}
			// {{env:NAME}} must be the entire arg, not embedded — avoid args like "FOO={{env:FOO}}"
			if arg != m[0] {
				return fmt.Errorf("env value in args must be a standalone arg, not embedded: %q", arg)
			}
			continue
		}
		if !allowedTokens[tok] {
			return fmt.Errorf("unknown token {{%s}}", tok)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run validator tests**

Run: `go test ./internal/api/handlers/ -run TestValidateMCPMetadata -v`
Expected: PASS.

- [ ] **Step 5: Wire validator into Create handler**

Modify `internal/api/handlers/packages.go` Create:
```go
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
	if pkg.Tags == nil {
		pkg.Tags = []string{}
	}
	if pkg.Type == domain.TypeMCP {
		if err := ValidateMCPMetadata(pkg.MCPMetadata); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if err := h.store.Create(r.Context(), &pkg); err != nil {
		http.Error(w, "failed to create package", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(pkg)
}
```

- [ ] **Step 6: Add handler-level tests for type=mcp**

Append to `internal/api/handlers/packages_test.go` (use existing test scaffold pattern):
```go
func TestPackagesCreate_MCP_OK(t *testing.T) {
	srv := newTestServer(t) // existing helper
	body := map[string]any{
		"name": "Gopedia MCP", "slug": "gopedia-mcp", "type": "mcp",
		"mcp_metadata": map[string]any{
			"server_name": "gopedia",
			"transport":   "stdio",
			"native":      map[string]any{"command": "gopedia-mcp-server"},
			"env_schema":  []map[string]any{{"name": "GOPEDIA_HOST_DOMAIN", "required": true}},
		},
	}
	resp := srv.POST(t, "/packages", body, withAuthToken("user1"))
	assert.Equal(t, http.StatusCreated, resp.Code)
}

func TestPackagesCreate_MCP_MissingMetadata(t *testing.T) {
	srv := newTestServer(t)
	body := map[string]any{"name": "x", "slug": "x", "type": "mcp"}
	resp := srv.POST(t, "/packages", body, withAuthToken("user1"))
	assert.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestPackagesGetBySlug_MCP_IncludesMetadata(t *testing.T) {
	srv := newTestServer(t)
	srv.MustCreatePackage(t, &domain.Package{
		Slug: "gopedia-mcp", Name: "x", Type: domain.TypeMCP,
		MCPMetadata: &domain.MCPMetadata{
			ServerName: "gopedia", Transport: "stdio",
			Native: &domain.MCPCommandSpec{Command: "gopedia-mcp-server"},
		},
	})
	resp := srv.GET(t, "/packages/gopedia-mcp")
	require.Equal(t, http.StatusOK, resp.Code)
	var pkg domain.Package
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &pkg))
	require.NotNil(t, pkg.MCPMetadata)
	assert.Equal(t, "gopedia", pkg.MCPMetadata.ServerName)
}
```

(If `newTestServer`/`withAuthToken` patterns differ, adapt to the existing harness — read `packages_test.go` first to match the style.)

- [ ] **Step 7: Run all handler tests**

Run: `go test ./internal/api/handlers/ -v`
Expected: PASS.

- [ ] **Step 8: Commit**
```bash
git add internal/api/handlers/mcp_validate.go internal/api/handlers/mcp_validate_test.go internal/api/handlers/packages.go internal/api/handlers/packages_test.go
git commit -m "feat: validate and persist mcp_metadata via API"
```

---

### Task A5: installscript LLM — mcp branch + forbidden-pattern post-validation

**Files:**
- Modify: `internal/installscript/llm.go`
- Modify: `internal/installscript/llm_test.go`

- [ ] **Step 1: Write failing test for mcp prompt branch**

Add to `internal/installscript/llm_test.go`:
```go
func TestBuildPrompt_MCPBranch(t *testing.T) {
	pkg := &domain.Package{Name: "Gopedia MCP", Slug: "gopedia-mcp", Type: domain.TypeMCP}
	p := buildPrompt(pkg, "1.0.0", "native")
	assert.Contains(t, p, "MCP client registration is handled by the morphso CLI")
	assert.NotContains(t, p, "docker run -d --name") // mcp docker is stdio, not daemon
}

func TestValidateGeneratedScript_RejectsClientRegistration(t *testing.T) {
	cases := []string{
		"#!/bin/sh\nclaude mcp add foo",
		"#!/bin/sh\necho ~/.claude.json",
		"#!/bin/sh\necho mcpServers",
		"#!/bin/sh\necho ~/.cursor/mcp.json",
		"#!/bin/sh\necho mcp.servers",
	}
	for _, c := range cases {
		assert.Error(t, validateGeneratedScript(c), "expected reject for %q", c)
	}
}

func TestValidateGeneratedScript_OK(t *testing.T) {
	ok := "#!/bin/sh\nnpm install -g gopedia-mcp-server@1.0.0\n"
	assert.NoError(t, validateGeneratedScript(ok))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/installscript/ -run "TestBuildPrompt_MCPBranch|TestValidateGeneratedScript" -v`
Expected: FAIL.

- [ ] **Step 3: Implement mcp branch in buildPrompt**

Replace the existing `buildPrompt` body in `llm.go`:
```go
func buildPrompt(pkg *domain.Package, version, strategy string) string {
	if pkg.Type == domain.TypeMCP {
		return fmt.Sprintf(
			`You are a shell script generator. Output a raw POSIX shell script and nothing else.

DO NOT include:
- Markdown code fences
- Any explanation or prose
- Any text before #!/bin/sh or after the last command
- Any MCP client registration commands (claude mcp ..., cursor, ~/.claude.json, ~/.cursor, ~/.gemini, mcpServers, mcp.servers).
  MCP client registration is handled by the morphso CLI, NOT this script.

The script must:
- Start with #!/bin/sh on the very first line
- Install "%s" (slug: %s, type: mcp) version %s using %s strategy
- native strategy: npm install -g <slug>@<version>
- docker strategy: docker pull <image>:<version> (stdio MCP servers are spawned by the client, do NOT run -d)
- Source MOSO_CONFIG if set: [ -n "$MOSO_CONFIG" ] && . "$MOSO_CONFIG"
- Work on macOS and Linux without modification

Begin your response with #!/bin/sh`,
			pkg.Name, pkg.Slug, version, strategy)
	}
	return fmt.Sprintf(
		`You are a shell script generator. Output a raw POSIX shell script and nothing else.

DO NOT include:
- Markdown code fences (no ` + "```" + ` or ` + "```" + `bash or ` + "```" + `sh)
- Any explanation, comments about the task, or prose
- Any text before #!/bin/sh or after the last command

The script must:
- Start with #!/bin/sh on the very first line
- Install "%s" (slug: %s, type: %s) version %s using %s strategy
- docker strategy: stop/remove existing container if present, then docker pull <image>, then docker run -d --name %s <image>
- Source MOSO_CONFIG if set: [ -n "$MOSO_CONFIG" ] && . "$MOSO_CONFIG"
- Work on macOS and Linux without modification

Begin your response with #!/bin/sh`,
		pkg.Name, pkg.Slug, string(pkg.Type), version, strategy, pkg.Slug)
}
```

- [ ] **Step 4: Implement validateGeneratedScript**

Append to `llm.go`:
```go
var forbiddenPatterns = []string{
	"claude mcp",
	"~/.claude.json",
	"mcpServers",
	"~/.cursor",
	"~/.gemini",
	"mcp.servers",
}

func validateGeneratedScript(script string) error {
	low := strings.ToLower(script)
	for _, p := range forbiddenPatterns {
		if strings.Contains(low, strings.ToLower(p)) {
			return fmt.Errorf("generated script contains forbidden pattern %q (MCP client registration belongs in CLI, not install script)", p)
		}
	}
	return nil
}
```

- [ ] **Step 5: Wire validation into the public generation entry**

Find the existing top-level function that returns the script (likely `GenerateInstallScript` on the Claude client struct). After `cleanScript(...)`, if `pkg.Type == domain.TypeMCP`, call `validateGeneratedScript(cleaned)` and return its error if non-nil. Show exact location: search for `cleanScript(` and add the gate immediately after.

```go
cleaned := cleanScript(raw)
if pkg.Type == domain.TypeMCP {
	if err := validateGeneratedScript(cleaned); err != nil {
		return "", err
	}
}
return cleaned, nil
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/installscript/ -v`
Expected: PASS (existing + new tests).

- [ ] **Step 7: Commit**
```bash
git add internal/installscript/llm.go internal/installscript/llm_test.go
git commit -m "feat: mcp-specific install script prompt + forbidden-pattern validation"
```

---

### Task A6: Run full hub test suite + DB migration smoke

- [ ] **Step 1: Run all hub tests**
```bash
go test ./...
```
Expected: PASS.

- [ ] **Step 2: Verify migration applies cleanly (manual, against dev DB)**

Document for reviewer (no script):
- `RunMigrations(<DEV_DB_URL>, "internal/db/migrations")` from a small Go script or existing CLI runner.
- `psql -c "\d packages"` shows `mcp_metadata | jsonb`.

(This is an out-of-band check before deploy — note in PR description, no commit.)

- [ ] **Step 3: Open PR for Phase A**

```bash
git push -u origin feat/mcp-package-type
gh pr create --title "feat: mcp package type — metadata column + validation" --body "$(cat <<'EOF'
## Summary

- Add `mcp_metadata` JSONB column to packages
- New `MCPMetadata` domain type + JSONB Scan/Value
- API validation: server_name regex, env name regex, transport allowlist, args templating token allowlist, forbid embedded {{env:}}
- LLM install-script generator: mcp-specific prompt + post-generation forbidden-pattern rejection

Sets up data plane for `moso install gopedia-mcp` (CLI changes in follow-up PR).

## Test plan

- [x] go test ./...
- [ ] Migration applied to dev DB, `\d packages` shows column
- [ ] POST /packages with type=mcp + valid metadata → 201; GET returns metadata
- [ ] POST /packages with type=mcp + invalid metadata → 400 with reason

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

Wait for review and merge before starting Phase B.

---

## Phase B — morphso (CLI)

Working directory for all Phase B tasks:
```
cd /Users/dong-hoshin/Documents/dev/morphso-wt-mcp-type
```

### Task B1: hub client types for MCPMetadata

**Files:**
- Modify: `internal/hub/types.go`
- Test: `internal/hub/parse_test.go`

- [ ] **Step 1: Write failing test**

Add to `internal/hub/parse_test.go`:
```go
func TestPackageJSON_WithMCPMetadata(t *testing.T) {
	jsonStr := `{
	  "slug":"gopedia-mcp","type":"mcp",
	  "mcp_metadata":{
	    "server_name":"gopedia","transport":"stdio",
	    "native":{"command":"gopedia-mcp-server"},
	    "env_schema":[{"name":"GOPEDIA_HOST_DOMAIN","required":true,"default":"127.0.0.1:18787"}]
	  }
	}`
	var p Package
	require.NoError(t, json.Unmarshal([]byte(jsonStr), &p))
	require.NotNil(t, p.MCPMetadata)
	assert.Equal(t, "gopedia", p.MCPMetadata.ServerName)
	assert.Equal(t, "stdio", p.MCPMetadata.Transport)
	assert.Equal(t, "gopedia-mcp-server", p.MCPMetadata.Native.Command)
	assert.Equal(t, "GOPEDIA_HOST_DOMAIN", p.MCPMetadata.EnvSchema[0].Name)
}
```

- [ ] **Step 2: Run test, expect FAIL**

Run: `go test ./internal/hub/ -run TestPackageJSON_WithMCPMetadata -v`
Expected: FAIL.

- [ ] **Step 3: Add types to `internal/hub/types.go`**

```go
type MCPMetadata struct {
	ServerName        string                  `json:"server_name"`
	Transport         string                  `json:"transport,omitempty"`
	Native            *MCPCommandSpec         `json:"native,omitempty"`
	Docker            *MCPCommandSpec         `json:"docker,omitempty"`
	PlatformOverrides map[string]MCPOverride  `json:"platform_overrides,omitempty"`
	EnvSchema         []MCPEnvSpec            `json:"env_schema,omitempty"`
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

Add field to existing `Package` struct in same file:
```go
	MCPMetadata *MCPMetadata `json:"mcp_metadata,omitempty"`
```

- [ ] **Step 4: Run test, expect PASS**

Run: `go test ./internal/hub/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**
```bash
git add internal/hub/types.go internal/hub/parse_test.go
git commit -m "feat: hub client types for MCPMetadata"
```

---

### Task B2: mcpclient interface + DetectInstalled scaffold

**Files:**
- Create: `internal/mcpclient/client.go`
- Create: `internal/mcpclient/client_test.go`

- [ ] **Step 1: Write failing test**

Create `internal/mcpclient/client_test.go`:
```go
package mcpclient

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAllClients_Names(t *testing.T) {
	names := []string{}
	for _, c := range AllClients() {
		names = append(names, c.Name())
	}
	assert.Contains(t, names, "claude-code")
	assert.Contains(t, names, "cursor")
	assert.Contains(t, names, "gemini-cli")
	assert.Len(t, names, 3)
}
```

- [ ] **Step 2: Run, expect FAIL (package undefined)**

Run: `go test ./internal/mcpclient/ -v`
Expected: FAIL.

- [ ] **Step 3: Create skeleton**

Create `internal/mcpclient/client.go`:
```go
package mcpclient

// MCPClient encapsulates per-client config layout. Each implementation owns
// its own JSON path (mcpServers vs mcp.servers) and any subprocess fallbacks.
type MCPClient interface {
	Name() string
	Detected() bool
	ExistingEntry(serverName string) (*MCPServerEntry, error)
	Register(serverName string, entry MCPServerEntry) error
	Unregister(serverName string) error
}

type MCPServerEntry struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// AllClients returns the canonical list. DetectInstalled() filters by Detected().
func AllClients() []MCPClient {
	return []MCPClient{
		newClaudeCode(),
		newCursor(),
		newGeminiCLI(),
	}
}

func DetectInstalled() []MCPClient {
	var out []MCPClient
	for _, c := range AllClients() {
		if c.Detected() {
			out = append(out, c)
		}
	}
	return out
}

// Stubs replaced by per-client files; declared here so compilation works
// while individual clients are added in subsequent tasks.
func newClaudeCode() MCPClient { return &stubClient{name: "claude-code"} }
func newCursor() MCPClient     { return &stubClient{name: "cursor"} }
func newGeminiCLI() MCPClient  { return &stubClient{name: "gemini-cli"} }

type stubClient struct{ name string }

func (s *stubClient) Name() string                                  { return s.name }
func (s *stubClient) Detected() bool                                { return false }
func (s *stubClient) ExistingEntry(string) (*MCPServerEntry, error) { return nil, nil }
func (s *stubClient) Register(string, MCPServerEntry) error         { return nil }
func (s *stubClient) Unregister(string) error                       { return nil }
```

(The stub constructors will be replaced one-by-one in B3–B5. Defining them up-front lets us land the interface first.)

- [ ] **Step 4: Run, expect PASS**

Run: `go test ./internal/mcpclient/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**
```bash
git add internal/mcpclient/client.go internal/mcpclient/client_test.go
git commit -m "feat: mcpclient interface + AllClients/DetectInstalled scaffold"
```

---

### Task B3: Cursor client implementation (flat mcpServers)

**Files:**
- Create: `internal/mcpclient/cursor.go`
- Create: `internal/mcpclient/cursor_test.go`
- Modify: `internal/mcpclient/client.go` (replace stub constructor)

- [ ] **Step 1: Write failing tests**

Create `internal/mcpclient/cursor_test.go`:
```go
package mcpclient

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cursorTempHome(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	t.Setenv("HOME", d)
	return d
}

func TestCursor_Detected_Absent(t *testing.T) {
	cursorTempHome(t)
	assert.False(t, newCursor().Detected())
}

func TestCursor_Detected_PresentDir(t *testing.T) {
	home := cursorTempHome(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cursor"), 0700))
	assert.True(t, newCursor().Detected())
}

func TestCursor_Register_CreatesFileWithFlatMCPServers(t *testing.T) {
	home := cursorTempHome(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".cursor"), 0700))

	c := newCursor()
	require.NoError(t, c.Register("gopedia", MCPServerEntry{
		Command: "gopedia-mcp-server",
		Args:    []string{},
		Env:     map[string]string{"GOPEDIA_HOST_DOMAIN": "127.0.0.1:18787"},
	}))

	data, err := os.ReadFile(filepath.Join(home, ".cursor", "mcp.json"))
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(data, &got))
	servers, _ := got["mcpServers"].(map[string]any)
	entry, _ := servers["gopedia"].(map[string]any)
	assert.Equal(t, "gopedia-mcp-server", entry["command"])
	env, _ := entry["env"].(map[string]any)
	assert.Equal(t, "127.0.0.1:18787", env["GOPEDIA_HOST_DOMAIN"])
}

func TestCursor_Register_PreservesOtherKeys(t *testing.T) {
	home := cursorTempHome(t)
	dir := filepath.Join(home, ".cursor")
	require.NoError(t, os.MkdirAll(dir, 0700))
	original := []byte(`{"mcpServers":{"existing":{"command":"old"}},"someOtherKey":42}`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mcp.json"), original, 0600))

	require.NoError(t, newCursor().Register("gopedia", MCPServerEntry{Command: "x"}))

	data, _ := os.ReadFile(filepath.Join(dir, "mcp.json"))
	var got map[string]any
	require.NoError(t, json.Unmarshal(data, &got))
	assert.EqualValues(t, 42, got["someOtherKey"])
	servers := got["mcpServers"].(map[string]any)
	assert.Contains(t, servers, "existing")
	assert.Contains(t, servers, "gopedia")
}

func TestCursor_Register_RefusesCorruptJSON(t *testing.T) {
	home := cursorTempHome(t)
	dir := filepath.Join(home, ".cursor")
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(`not json`), 0600))

	err := newCursor().Register("gopedia", MCPServerEntry{Command: "x"})
	assert.ErrorContains(t, err, "corrupt")
}

func TestCursor_Unregister_Idempotent(t *testing.T) {
	cursorTempHome(t)
	c := newCursor()
	assert.NoError(t, c.Unregister("never-existed"))

	// existing case
	home := cursorTempHome(t)
	dir := filepath.Join(home, ".cursor")
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, newCursor().Register("foo", MCPServerEntry{Command: "x"}))
	require.NoError(t, newCursor().Unregister("foo"))
	data, _ := os.ReadFile(filepath.Join(dir, "mcp.json"))
	var got map[string]any
	json.Unmarshal(data, &got)
	servers, _ := got["mcpServers"].(map[string]any)
	assert.NotContains(t, servers, "foo")
}

func TestCursor_ExistingEntry(t *testing.T) {
	home := cursorTempHome(t)
	dir := filepath.Join(home, ".cursor")
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, newCursor().Register("gopedia", MCPServerEntry{
		Command: "x", Env: map[string]string{"A": "1"},
	}))
	got, err := newCursor().ExistingEntry("gopedia")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "x", got.Command)
	assert.Equal(t, "1", got.Env["A"])

	missing, err := newCursor().ExistingEntry("nope")
	require.NoError(t, err)
	assert.Nil(t, missing)
}
```

- [ ] **Step 2: Run, expect FAIL**

Run: `go test ./internal/mcpclient/ -run TestCursor -v`
Expected: FAIL (newCursor returns stub).

- [ ] **Step 3: Implement Cursor client**

Create `internal/mcpclient/cursor.go`:
```go
package mcpclient

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type cursorClient struct{}

func newCursorReal() MCPClient { return &cursorClient{} }

func (c *cursorClient) Name() string { return "cursor" }

func (c *cursorClient) configPath() string {
	return filepath.Join(os.Getenv("HOME"), ".cursor", "mcp.json")
}

func (c *cursorClient) Detected() bool {
	_, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".cursor"))
	return err == nil
}

func (c *cursorClient) ExistingEntry(name string) (*MCPServerEntry, error) {
	cfg, err := readJSONConfig(c.configPath())
	if err != nil {
		return nil, err
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		return nil, nil
	}
	raw, ok := servers[name]
	if !ok {
		return nil, nil
	}
	return decodeEntry(raw)
}

func (c *cursorClient) Register(name string, entry MCPServerEntry) error {
	cfg, err := readJSONConfig(c.configPath())
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers[name] = entry
	cfg["mcpServers"] = servers
	return writeJSONConfigAtomic(c.configPath(), cfg)
}

func (c *cursorClient) Unregister(name string) error {
	cfg, err := readJSONConfig(c.configPath())
	if err != nil {
		return err
	}
	if cfg == nil {
		return nil
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		return nil
	}
	delete(servers, name)
	cfg["mcpServers"] = servers
	return writeJSONConfigAtomic(c.configPath(), cfg)
}

// --- shared helpers (placed here for now; Task B6 may extract to fsutil.go) ---

func readJSONConfig(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("corrupt config at %s: %w (refusing to overwrite)", path, err)
	}
	return cfg, nil
}

func writeJSONConfigAtomic(path string, cfg map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	// best-effort backup
	if data, err := os.ReadFile(path); err == nil {
		_ = os.WriteFile(path+".bak", data, 0600)
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mcp-*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	tmp.Close()
	if err := os.Chmod(tmp.Name(), 0600); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func decodeEntry(raw any) (*MCPServerEntry, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var e MCPServerEntry
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, err
	}
	return &e, nil
}
```

- [ ] **Step 4: Wire constructor**

In `internal/mcpclient/client.go`, replace `func newCursor() MCPClient { return &stubClient{name: "cursor"} }` with:
```go
func newCursor() MCPClient { return newCursorReal() }
```

- [ ] **Step 5: Run tests, expect PASS**

Run: `go test ./internal/mcpclient/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**
```bash
git add internal/mcpclient/cursor.go internal/mcpclient/cursor_test.go internal/mcpclient/client.go
git commit -m "feat: mcpclient cursor implementation"
```

---

### Task B4: Gemini CLI client implementation (nested mcp.servers)

**Files:**
- Create: `internal/mcpclient/gemini_cli.go`
- Create: `internal/mcpclient/gemini_cli_test.go`
- Modify: `internal/mcpclient/client.go`

- [ ] **Step 1: Write failing tests**

Create `internal/mcpclient/gemini_cli_test.go`:
```go
package mcpclient

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func geminiTempHome(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	t.Setenv("HOME", d)
	return d
}

func TestGemini_Detected_DirExists(t *testing.T) {
	home := geminiTempHome(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".gemini"), 0700))
	assert.True(t, newGeminiCLI().Detected())
}

func TestGemini_Register_NestedMCPDotServers(t *testing.T) {
	home := geminiTempHome(t)
	dir := filepath.Join(home, ".gemini")
	require.NoError(t, os.MkdirAll(dir, 0700))

	require.NoError(t, newGeminiCLI().Register("gopedia", MCPServerEntry{
		Command: "gopedia-mcp-server",
		Env:     map[string]string{"X": "1"},
	}))

	data, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var got map[string]any
	require.NoError(t, json.Unmarshal(data, &got))
	mcp, _ := got["mcp"].(map[string]any)
	servers, _ := mcp["servers"].(map[string]any)
	entry, _ := servers["gopedia"].(map[string]any)
	assert.Equal(t, "gopedia-mcp-server", entry["command"])
}

func TestGemini_Register_PreservesOtherTopLevelKeys(t *testing.T) {
	home := geminiTempHome(t)
	dir := filepath.Join(home, ".gemini")
	require.NoError(t, os.MkdirAll(dir, 0700))
	original := []byte(`{"ide":{"hasSeenNudge":true},"security":{"auth":"x"}}`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "settings.json"), original, 0600))

	require.NoError(t, newGeminiCLI().Register("gopedia", MCPServerEntry{Command: "x"}))

	var got map[string]any
	data, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Contains(t, got, "ide")
	assert.Contains(t, got, "security")
	assert.Contains(t, got, "mcp")
}

func TestGemini_RefusesCorruptJSON(t *testing.T) {
	home := geminiTempHome(t)
	dir := filepath.Join(home, ".gemini")
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "settings.json"), []byte("oops"), 0600))

	err := newGeminiCLI().Register("gopedia", MCPServerEntry{Command: "x"})
	assert.ErrorContains(t, err, "corrupt")
}

func TestGemini_Unregister_Idempotent(t *testing.T) {
	home := geminiTempHome(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".gemini"), 0700))
	assert.NoError(t, newGeminiCLI().Unregister("missing"))
}
```

- [ ] **Step 2: Run, expect FAIL**

Run: `go test ./internal/mcpclient/ -run TestGemini -v`
Expected: FAIL.

- [ ] **Step 3: Implement Gemini client**

Create `internal/mcpclient/gemini_cli.go`:
```go
package mcpclient

import (
	"os"
	"path/filepath"
)

type geminiCLIClient struct{}

func newGeminiCLIReal() MCPClient { return &geminiCLIClient{} }

func (g *geminiCLIClient) Name() string { return "gemini-cli" }

func (g *geminiCLIClient) configPath() string {
	return filepath.Join(os.Getenv("HOME"), ".gemini", "settings.json")
}

func (g *geminiCLIClient) Detected() bool {
	_, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".gemini"))
	return err == nil
}

func (g *geminiCLIClient) ExistingEntry(name string) (*MCPServerEntry, error) {
	cfg, err := readJSONConfig(g.configPath())
	if err != nil {
		return nil, err
	}
	servers := g.servers(cfg)
	if servers == nil {
		return nil, nil
	}
	raw, ok := servers[name]
	if !ok {
		return nil, nil
	}
	return decodeEntry(raw)
}

func (g *geminiCLIClient) Register(name string, entry MCPServerEntry) error {
	cfg, err := readJSONConfig(g.configPath())
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	mcp, _ := cfg["mcp"].(map[string]any)
	if mcp == nil {
		mcp = map[string]any{}
	}
	servers, _ := mcp["servers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers[name] = entry
	mcp["servers"] = servers
	cfg["mcp"] = mcp
	return writeJSONConfigAtomic(g.configPath(), cfg)
}

func (g *geminiCLIClient) Unregister(name string) error {
	cfg, err := readJSONConfig(g.configPath())
	if err != nil {
		return err
	}
	if cfg == nil {
		return nil
	}
	servers := g.servers(cfg)
	if servers == nil {
		return nil
	}
	delete(servers, name)
	mcp, _ := cfg["mcp"].(map[string]any)
	mcp["servers"] = servers
	cfg["mcp"] = mcp
	return writeJSONConfigAtomic(g.configPath(), cfg)
}

func (g *geminiCLIClient) servers(cfg map[string]any) map[string]any {
	mcp, _ := cfg["mcp"].(map[string]any)
	if mcp == nil {
		return nil
	}
	s, _ := mcp["servers"].(map[string]any)
	return s
}
```

- [ ] **Step 4: Wire constructor**

In `client.go`:
```go
func newGeminiCLI() MCPClient { return newGeminiCLIReal() }
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/mcpclient/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**
```bash
git add internal/mcpclient/gemini_cli.go internal/mcpclient/gemini_cli_test.go internal/mcpclient/client.go
git commit -m "feat: mcpclient gemini-cli implementation (mcp.servers nested)"
```

---

### Task B5: Claude Code client implementation (subprocess + direct edit fallback)

**Files:**
- Create: `internal/mcpclient/claude_code.go`
- Create: `internal/mcpclient/claude_code_test.go`
- Modify: `internal/mcpclient/client.go`

- [ ] **Step 1: Write failing tests**

Create `internal/mcpclient/claude_code_test.go`:
```go
package mcpclient

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// In tests we always exercise the direct-edit fallback by setting
// MOSO_DISABLE_CLAUDE_SUBPROCESS=1 — the subprocess path is exercised via
// integration tests outside this unit suite.
func claudeTempHome(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	t.Setenv("HOME", d)
	t.Setenv("MOSO_DISABLE_CLAUDE_SUBPROCESS", "1")
	return d
}

func TestClaude_Detected_FilePresent(t *testing.T) {
	home := claudeTempHome(t)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{}`), 0600))
	assert.True(t, newClaudeCode().Detected())
}

func TestClaude_Register_WritesUserScopeMCPServers(t *testing.T) {
	home := claudeTempHome(t)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{}`), 0600))

	require.NoError(t, newClaudeCode().Register("gopedia", MCPServerEntry{
		Command: "gopedia-mcp-server",
		Env:     map[string]string{"GOPEDIA_HOST_DOMAIN": "127.0.0.1:18787"},
	}))

	data, _ := os.ReadFile(filepath.Join(home, ".claude.json"))
	var got map[string]any
	require.NoError(t, json.Unmarshal(data, &got))
	servers, _ := got["mcpServers"].(map[string]any)
	require.NotNil(t, servers)
	entry, _ := servers["gopedia"].(map[string]any)
	assert.Equal(t, "gopedia-mcp-server", entry["command"])
}

func TestClaude_Register_PreservesExistingKeys(t *testing.T) {
	home := claudeTempHome(t)
	original := []byte(`{"theme":"dark","numStartups":42,"projects":{"/tmp":{"x":1}}}`)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".claude.json"), original, 0600))

	require.NoError(t, newClaudeCode().Register("gopedia", MCPServerEntry{Command: "x"}))

	var got map[string]any
	data, _ := os.ReadFile(filepath.Join(home, ".claude.json"))
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "dark", got["theme"])
	assert.EqualValues(t, 42, got["numStartups"])
	assert.Contains(t, got, "projects")
}

func TestClaude_RefusesCorruptJSON(t *testing.T) {
	home := claudeTempHome(t)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".claude.json"), []byte("broken"), 0600))
	err := newClaudeCode().Register("gopedia", MCPServerEntry{Command: "x"})
	assert.ErrorContains(t, err, "corrupt")
}

func TestClaude_Unregister_Idempotent(t *testing.T) {
	claudeTempHome(t)
	assert.NoError(t, newClaudeCode().Unregister("nope"))
}
```

- [ ] **Step 2: Run, expect FAIL**

Run: `go test ./internal/mcpclient/ -run TestClaude -v`
Expected: FAIL.

- [ ] **Step 3: Implement Claude Code client**

Create `internal/mcpclient/claude_code.go`:
```go
package mcpclient

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
)

type claudeCodeClient struct{}

func newClaudeCodeReal() MCPClient { return &claudeCodeClient{} }

func (c *claudeCodeClient) Name() string { return "claude-code" }

func (c *claudeCodeClient) configPath() string {
	return filepath.Join(os.Getenv("HOME"), ".claude.json")
}

func (c *claudeCodeClient) Detected() bool {
	if _, err := os.Stat(c.configPath()); err == nil {
		return true
	}
	_, err := exec.LookPath("claude")
	return err == nil
}

func (c *claudeCodeClient) ExistingEntry(name string) (*MCPServerEntry, error) {
	cfg, err := readJSONConfig(c.configPath())
	if err != nil || cfg == nil {
		return nil, err
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		return nil, nil
	}
	raw, ok := servers[name]
	if !ok {
		return nil, nil
	}
	return decodeEntry(raw)
}

func (c *claudeCodeClient) Register(name string, entry MCPServerEntry) error {
	// Prefer subprocess: claude CLI handles scope precedence correctly.
	// Tests disable this via MOSO_DISABLE_CLAUDE_SUBPROCESS=1.
	if os.Getenv("MOSO_DISABLE_CLAUDE_SUBPROCESS") == "" {
		if _, err := exec.LookPath("claude"); err == nil {
			if err := c.registerViaCLI(name, entry); err == nil {
				return nil
			}
			// fall through to direct edit on subprocess failure
		}
	}
	return c.registerDirect(name, entry)
}

func (c *claudeCodeClient) registerViaCLI(name string, entry MCPServerEntry) error {
	jsonBlob, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	cmd := exec.Command("claude", "mcp", "add-json", name, string(jsonBlob), "--scope", "user")
	cmd.Stdout = os.Stderr // surface to user; not stdout to avoid mixing with moso output
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (c *claudeCodeClient) registerDirect(name string, entry MCPServerEntry) error {
	cfg, err := readJSONConfig(c.configPath())
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers[name] = entry
	cfg["mcpServers"] = servers
	return writeJSONConfigAtomic(c.configPath(), cfg)
}

func (c *claudeCodeClient) Unregister(name string) error {
	if os.Getenv("MOSO_DISABLE_CLAUDE_SUBPROCESS") == "" {
		if _, err := exec.LookPath("claude"); err == nil {
			cmd := exec.Command("claude", "mcp", "remove", name, "--scope", "user")
			cmd.Stdout = os.Stderr
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err == nil {
				return nil
			}
		}
	}
	cfg, err := readJSONConfig(c.configPath())
	if err != nil || cfg == nil {
		return err
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		return nil
	}
	delete(servers, name)
	cfg["mcpServers"] = servers
	return writeJSONConfigAtomic(c.configPath(), cfg)
}
```

- [ ] **Step 4: Wire constructor**

In `client.go`:
```go
func newClaudeCode() MCPClient { return newClaudeCodeReal() }
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/mcpclient/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**
```bash
git add internal/mcpclient/claude_code.go internal/mcpclient/claude_code_test.go internal/mcpclient/client.go
git commit -m "feat: mcpclient claude-code with subprocess + direct edit fallback"
```

---

### Task B6: Split mcp out of installer fallthrough

**Files:**
- Modify: `internal/installer/installer.go`
- Modify: `internal/installer/installer_test.go`

- [ ] **Step 1: Write failing test**

Add to `internal/installer/installer_test.go`:
```go
func TestBuildCommand_MCP_Native_UsesNpm(t *testing.T) {
	got := BuildCommand("mcp", "native", "gopedia-mcp", "1.0.0")
	assert.Equal(t, []string{"npm", "install", "-g", "gopedia-mcp@1.0.0"}, got)
}

func TestBuildCommand_PipUnchanged(t *testing.T) {
	got := BuildCommand("pip", "native", "x", "")
	assert.Equal(t, []string{"pip", "install", "x"}, got)
}
```

- [ ] **Step 2: Run, expect FAIL**

Run: `go test ./internal/installer/ -run TestBuildCommand_MCP_Native_UsesNpm -v`
Expected: FAIL (current code returns pip command for mcp).

- [ ] **Step 3: Update buildNative**

In `internal/installer/installer.go` modify `buildNative`:
```go
func buildNative(pkgType, slug, version string) []string {
	switch pkgType {
	case "npm":
		pkg := slug
		if version != "" {
			pkg += "@" + version
		}
		return []string{"npm", "install", "-g", pkg}
	case "mcp":
		pkg := slug
		if version != "" {
			pkg += "@" + version
		}
		return []string{"npm", "install", "-g", pkg}
	case "brew":
		return []string{"brew", "install", slug}
	case "pip", "recipe", "binary", "helm":
		fallthrough
	default:
		pkg := slug
		if version != "" {
			pkg += "==" + version
		}
		return []string{"pip", "install", pkg}
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/installer/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**
```bash
git add internal/installer/installer.go internal/installer/installer_test.go
git commit -m "feat: installer mcp fallback uses npm install -g"
```

---

### Task B7: LocalRecommend mcp branch

**Files:**
- Modify: `internal/hub/deps.go`
- Modify: `internal/hub/deps_test.go`

- [ ] **Step 1: Read current LocalRecommend**

Skim `internal/hub/deps.go` to find `LocalRecommend(s *spec.Spec, preferred string) string`. Note signature.

- [ ] **Step 2: Write failing test**

Add to `internal/hub/deps_test.go`:
```go
func TestLocalRecommend_MCP_PrefersNativeWhenNpmAvailable(t *testing.T) {
	// Skip if npm is genuinely missing in CI — this test exercises the branch
	// when LocalRecommend is called for an mcp-type package.
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm not installed; skipping")
	}
	s := &spec.Spec{OS: "darwin", Arch: "arm64"}
	// LocalRecommend currently doesn't know package type. We extend its signature
	// (see Step 3) or add a helper RecommendForType.
	got := RecommendForType("mcp", s, "")
	assert.Equal(t, "native", got)
}
```

- [ ] **Step 3: Add RecommendForType helper**

In `internal/hub/deps.go`:
```go
import "os/exec"

// RecommendForType returns a strategy for a given package type, preferring
// `preferred` if non-empty. For mcp packages, prefers native (npm) when
// npm is available, falls back to docker.
func RecommendForType(pkgType string, s *spec.Spec, preferred string) string {
	if preferred != "" {
		return preferred
	}
	switch pkgType {
	case "mcp":
		if _, err := exec.LookPath("npm"); err == nil {
			return "native"
		}
		if _, err := exec.LookPath("docker"); err == nil {
			return "docker"
		}
		return "native" // explicit error happens later via prerequisite check
	default:
		return LocalRecommend(s, preferred)
	}
}
```

- [ ] **Step 4: Run test**

Run: `go test ./internal/hub/ -run TestLocalRecommend_MCP -v`
Expected: PASS.

- [ ] **Step 5: Wire into runInstall**

In `cmd/install.go` `runInstall`, locate:
```go
if strategy == "" {
    strategy = hub.LocalRecommend(s, preferred)
    reason = "로컬 rule-based 추천 (hub 미연결 또는 미로그인)"
}
```

Replace with:
```go
if strategy == "" {
    strategy = hub.RecommendForType(string(pkg.Type), s, preferred)
    reason = "로컬 rule-based 추천 (hub 미연결 또는 미로그인)"
}
```

- [ ] **Step 6: Run all tests**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 7: Commit**
```bash
git add internal/hub/deps.go internal/hub/deps_test.go cmd/install.go
git commit -m "feat: RecommendForType prefers native(npm) for mcp packages"
```

---

### Task B8: token expansion utility

**Files:**
- Create: `internal/mcpclient/tokens.go`
- Create: `internal/mcpclient/tokens_test.go`

- [ ] **Step 1: Write failing test**

Create `internal/mcpclient/tokens_test.go`:
```go
package mcpclient

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandTokens_Image(t *testing.T) {
	ctx := TokenContext{Image: "repo/img", Version: "1.0.0", Slug: "gopedia-mcp"}
	got, err := ExpandTokens([]string{"run", "{{image}}"}, ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"run", "repo/img:1.0.0"}, got)
}

func TestExpandTokens_VersionAndSlug(t *testing.T) {
	ctx := TokenContext{Slug: "x", Version: "2"}
	got, err := ExpandTokens([]string{"--name", "{{slug}}-{{version}}"}, ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"--name", "x-2"}, got)
}

func TestExpandTokens_UnknownTokenError(t *testing.T) {
	_, err := ExpandTokens([]string{"{{unknown}}"}, TokenContext{})
	assert.ErrorContains(t, err, "unknown token")
}
```

- [ ] **Step 2: Run, expect FAIL**

Run: `go test ./internal/mcpclient/ -run TestExpandTokens -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

Create `internal/mcpclient/tokens.go`:
```go
package mcpclient

import (
	"fmt"
	"regexp"
)

type TokenContext struct {
	Image   string
	Version string
	Slug    string
}

var tokenPattern = regexp.MustCompile(`\{\{([a-z0-9_:-]+)\}\}`)

func ExpandTokens(args []string, ctx TokenContext) ([]string, error) {
	out := make([]string, len(args))
	for i, a := range args {
		expanded, err := expand(a, ctx)
		if err != nil {
			return nil, err
		}
		out[i] = expanded
	}
	return out, nil
}

func expand(s string, ctx TokenContext) (string, error) {
	var resErr error
	result := tokenPattern.ReplaceAllStringFunc(s, func(match string) string {
		name := match[2 : len(match)-2]
		switch name {
		case "image":
			if ctx.Version == "" {
				return ctx.Image
			}
			return ctx.Image + ":" + ctx.Version
		case "version":
			return ctx.Version
		case "slug":
			return ctx.Slug
		default:
			if resErr == nil {
				resErr = fmt.Errorf("unknown token %s", match)
			}
			return match
		}
	})
	return result, resErr
}
```

Note: `{{env:NAME}}` is intentionally **not** supported in CLI expansion — the spec forbids embedding env values in args (hub validator rejects it).

- [ ] **Step 4: Run tests**

Run: `go test ./internal/mcpclient/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**
```bash
git add internal/mcpclient/tokens.go internal/mcpclient/tokens_test.go
git commit -m "feat: token expansion for mcp args ({{image}}, {{version}}, {{slug}})"
```

---

### Task B9: runMCPRegistration in cmd/install.go

**Files:**
- Modify: `cmd/install.go`
- Create: `cmd/install_mcp.go`
- Create: `cmd/install_mcp_test.go`

- [ ] **Step 1: Write failing tests for the registration helper**

Create `cmd/install_mcp_test.go`:
```go
package cmd

import (
	"bufio"
	"bytes"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tojiuni/morphso/internal/hub"
	"github.com/tojiuni/morphso/internal/mcpclient"
)

// fakeClient implements mcpclient.MCPClient for assertions.
type fakeClient struct {
	name        string
	detected    bool
	existing    *mcpclient.MCPServerEntry
	registered  map[string]mcpclient.MCPServerEntry
	unregistered []string
}

func (f *fakeClient) Name() string         { return f.name }
func (f *fakeClient) Detected() bool       { return f.detected }
func (f *fakeClient) ExistingEntry(string) (*mcpclient.MCPServerEntry, error) {
	return f.existing, nil
}
func (f *fakeClient) Register(name string, e mcpclient.MCPServerEntry) error {
	if f.registered == nil {
		f.registered = map[string]mcpclient.MCPServerEntry{}
	}
	f.registered[name] = e
	return nil
}
func (f *fakeClient) Unregister(name string) error {
	f.unregistered = append(f.unregistered, name)
	return nil
}

func TestRunMCPRegistration_NativeBasic(t *testing.T) {
	meta := &hub.MCPMetadata{
		ServerName: "gopedia", Transport: "stdio",
		Native: &hub.MCPCommandSpec{Command: "gopedia-mcp-server"},
		EnvSchema: []hub.MCPEnvSpec{
			{Name: "GOPEDIA_HOST_DOMAIN", Required: true, Default: "127.0.0.1:18787"},
		},
	}
	pkg := &hub.Package{Slug: "gopedia-mcp", Type: "mcp", MCPMetadata: meta}
	client := &fakeClient{name: "fake", detected: true}

	reader := bufio.NewReader(strings.NewReader("\n")) // accept default
	out := &bytes.Buffer{}
	err := runMCPRegistrationWithClients(pkg, "native", "1.0.0", reader, out,
		[]mcpclient.MCPClient{client}, false, false)
	require.NoError(t, err)

	got, ok := client.registered["gopedia"]
	require.True(t, ok)
	assert.Equal(t, "gopedia-mcp-server", got.Command)
	assert.Equal(t, "127.0.0.1:18787", got.Env["GOPEDIA_HOST_DOMAIN"])
}

func TestRunMCPRegistration_TransportUnsupported(t *testing.T) {
	pkg := &hub.Package{Slug: "x", Type: "mcp", MCPMetadata: &hub.MCPMetadata{
		ServerName: "x", Transport: "http",
		Native: &hub.MCPCommandSpec{Command: "x"},
	}}
	err := runMCPRegistrationWithClients(pkg, "native", "1", bufio.NewReader(strings.NewReader("")), &bytes.Buffer{}, nil, true, false)
	assert.ErrorContains(t, err, "transport")
}

func TestRunMCPRegistration_WindowsSkip(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only path")
	}
	// runMCPRegistration on windows should skip without error.
	pkg := &hub.Package{Slug: "x", Type: "mcp", MCPMetadata: &hub.MCPMetadata{
		ServerName: "x", Native: &hub.MCPCommandSpec{Command: "x"},
	}}
	out := &bytes.Buffer{}
	err := runMCPRegistrationWithClients(pkg, "native", "1", bufio.NewReader(strings.NewReader("")), out, nil, true, false)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "windows registration not implemented")
}

func TestRunMCPRegistration_ReusesExistingEnvAsDefault(t *testing.T) {
	meta := &hub.MCPMetadata{
		ServerName: "gopedia", Transport: "stdio",
		Native: &hub.MCPCommandSpec{Command: "x"},
		EnvSchema: []hub.MCPEnvSpec{{Name: "FOO", Required: true}},
	}
	pkg := &hub.Package{Slug: "x", Type: "mcp", MCPMetadata: meta}
	client := &fakeClient{name: "f", detected: true,
		existing: &mcpclient.MCPServerEntry{Command: "x", Env: map[string]string{"FOO": "old-value"}},
	}
	reader := bufio.NewReader(strings.NewReader("\n")) // accept default = existing
	err := runMCPRegistrationWithClients(pkg, "native", "1", reader, &bytes.Buffer{}, []mcpclient.MCPClient{client}, false, false)
	require.NoError(t, err)
	assert.Equal(t, "old-value", client.registered["gopedia"].Env["FOO"])
}

func TestRunMCPRegistration_YesUsesEnvFallback(t *testing.T) {
	t.Setenv("FOO", "from-env")
	meta := &hub.MCPMetadata{
		ServerName: "x", Transport: "stdio",
		Native: &hub.MCPCommandSpec{Command: "x"},
		EnvSchema: []hub.MCPEnvSpec{{Name: "FOO", Required: true}},
	}
	pkg := &hub.Package{Slug: "x", Type: "mcp", MCPMetadata: meta}
	client := &fakeClient{name: "f", detected: true}
	err := runMCPRegistrationWithClients(pkg, "native", "1", bufio.NewReader(strings.NewReader("")), &bytes.Buffer{}, []mcpclient.MCPClient{client}, true, false)
	require.NoError(t, err)
	assert.Equal(t, "from-env", client.registered["x"].Env["FOO"])
}

func TestRunMCPRegistration_MissingRequiredYesErrors(t *testing.T) {
	meta := &hub.MCPMetadata{
		ServerName: "x", Native: &hub.MCPCommandSpec{Command: "x"},
		EnvSchema: []hub.MCPEnvSpec{{Name: "FOO", Required: true}},
	}
	pkg := &hub.Package{Slug: "x", Type: "mcp", MCPMetadata: meta}
	err := runMCPRegistrationWithClients(pkg, "native", "1", bufio.NewReader(strings.NewReader("")), &bytes.Buffer{}, []mcpclient.MCPClient{&fakeClient{detected: true}}, true, false)
	assert.ErrorContains(t, err, "FOO")
}

func TestRunMCPRegistration_DockerImageTokenExpanded(t *testing.T) {
	meta := &hub.MCPMetadata{
		ServerName: "x", Transport: "stdio",
		Docker: &hub.MCPCommandSpec{Image: "repo/img", Args: []string{"run", "-i", "--rm", "{{image}}"}},
	}
	pkg := &hub.Package{Slug: "x", Type: "mcp", MCPMetadata: meta}
	client := &fakeClient{detected: true}
	err := runMCPRegistrationWithClients(pkg, "docker", "1.0", bufio.NewReader(strings.NewReader("")), &bytes.Buffer{}, []mcpclient.MCPClient{client}, true, false)
	require.NoError(t, err)
	got := client.registered["x"]
	assert.Equal(t, "docker", got.Command)
	assert.Equal(t, []string{"run", "-i", "--rm", "repo/img:1.0"}, got.Args)
}

func TestRunMCPRegistration_NoOutputContainsEnvValue(t *testing.T) {
	t.Setenv("SECRET_KEY", "supersecret123")
	meta := &hub.MCPMetadata{
		ServerName: "x", Transport: "stdio",
		Native: &hub.MCPCommandSpec{Command: "x"},
		EnvSchema: []hub.MCPEnvSpec{{Name: "SECRET_KEY", Required: true, Secret: true}},
	}
	pkg := &hub.Package{Slug: "x", Type: "mcp", MCPMetadata: meta}
	out := &bytes.Buffer{}
	err := runMCPRegistrationWithClients(pkg, "native", "1", bufio.NewReader(strings.NewReader("")), out, []mcpclient.MCPClient{&fakeClient{detected: true}}, true, false)
	require.NoError(t, err)
	assert.NotContains(t, out.String(), "supersecret123")
}
```

- [ ] **Step 2: Run, expect FAIL**

Run: `go test ./cmd/ -run TestRunMCPRegistration -v`
Expected: FAIL.

- [ ] **Step 3: Implement registration helper**

Create `cmd/install_mcp.go`:
```go
package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/tojiuni/morphso/internal/hub"
	"github.com/tojiuni/morphso/internal/mcpclient"
)

// runMCPRegistration is the production entry point; runMCPRegistrationWithClients is the testable core.
func runMCPRegistration(pkg *hub.Package, strategy, version string, reader *bufio.Reader, out io.Writer, yes, reconfigure bool) error {
	clients := mcpclient.DetectInstalled()
	return runMCPRegistrationWithClients(pkg, strategy, version, reader, out, clients, yes, reconfigure)
}

func runMCPRegistrationWithClients(
	pkg *hub.Package, strategy, version string,
	reader *bufio.Reader, out io.Writer,
	clients []mcpclient.MCPClient,
	yes, reconfigure bool,
) error {
	meta := pkg.MCPMetadata
	if meta == nil {
		return fmt.Errorf("mcp package missing mcp_metadata")
	}
	if meta.Transport != "" && meta.Transport != "stdio" {
		return fmt.Errorf("transport %q not supported in v1 (stdio only)", meta.Transport)
	}
	if runtime.GOOS == "windows" {
		fmt.Fprintln(out, "windows registration not implemented in v1; skipping MCP client registration")
		return nil
	}

	// Resolve platform overrides
	resolved := applyOverride(meta, runtime.GOOS)

	// Gather existing env from any detected client (first hit wins).
	existing := map[string]string{}
	if !reconfigure {
		for _, c := range clients {
			e, err := c.ExistingEntry(meta.ServerName)
			if err != nil || e == nil {
				continue
			}
			for k, v := range e.Env {
				if _, ok := existing[k]; !ok {
					existing[k] = v
				}
			}
		}
	}

	collected, err := collectEnv(meta.EnvSchema, existing, reader, out, yes)
	if err != nil {
		return err
	}

	entry, err := buildEntry(resolved, strategy, version, pkg.Slug, collected)
	if err != nil {
		return err
	}

	for _, c := range clients {
		if !c.Detected() {
			fmt.Fprintf(out, "· %s: 미감지, skip\n", c.Name())
			continue
		}
		if err := c.Register(meta.ServerName, entry); err != nil {
			fmt.Fprintf(out, "✗ %s: 등록 실패: %v\n", c.Name(), err)
			continue
		}
		fmt.Fprintf(out, "✓ %s에 '%s' 등록\n", c.Name(), meta.ServerName)
	}
	return nil
}

func applyOverride(m *hub.MCPMetadata, goos string) *hub.MCPMetadata {
	if m.PlatformOverrides == nil {
		return m
	}
	ov, ok := m.PlatformOverrides[goos]
	if !ok {
		return m
	}
	resolved := *m
	if ov.Native != nil {
		resolved.Native = ov.Native
	}
	if ov.Docker != nil {
		resolved.Docker = ov.Docker
	}
	return &resolved
}

func collectEnv(schema []hub.MCPEnvSpec, existing map[string]string, reader *bufio.Reader, out io.Writer, yes bool) (map[string]string, error) {
	collected := map[string]string{}
	for _, e := range schema {
		defaultVal := existing[e.Name]
		if defaultVal == "" {
			defaultVal = e.Default
		}
		if yes {
			val := os.Getenv(e.Name)
			if val == "" {
				val = defaultVal
			}
			if val == "" && e.Required {
				return nil, fmt.Errorf("required env %s not provided (--yes mode)", e.Name)
			}
			if val != "" {
				collected[e.Name] = val
			}
			continue
		}
		display := defaultVal
		if e.Secret && display != "" {
			display = "***"
		}
		prompt := e.Prompt
		if prompt == "" {
			prompt = e.Name
		}
		if defaultVal != "" {
			fmt.Fprintf(out, "%s [기본: %s]: ", prompt, display)
		} else {
			fmt.Fprintf(out, "%s: ", prompt)
		}
		raw, _ := reader.ReadString('\n')
		raw = strings.TrimRight(raw, "\r\n")
		if raw == "" {
			raw = defaultVal
		}
		if raw == "" && e.Required {
			return nil, fmt.Errorf("required env %s not provided", e.Name)
		}
		if raw != "" {
			collected[e.Name] = raw
		}
	}
	return collected, nil
}

func buildEntry(m *hub.MCPMetadata, strategy, version, slug string, env map[string]string) (mcpclient.MCPServerEntry, error) {
	ctx := mcpclient.TokenContext{Version: version, Slug: slug}
	switch strategy {
	case "native":
		if m.Native == nil {
			return mcpclient.MCPServerEntry{}, fmt.Errorf("native spec missing")
		}
		args, err := mcpclient.ExpandTokens(m.Native.Args, ctx)
		if err != nil {
			return mcpclient.MCPServerEntry{}, err
		}
		return mcpclient.MCPServerEntry{Command: m.Native.Command, Args: args, Env: env}, nil
	case "docker":
		if m.Docker == nil {
			return mcpclient.MCPServerEntry{}, fmt.Errorf("docker spec missing")
		}
		ctx.Image = m.Docker.Image
		args, err := mcpclient.ExpandTokens(m.Docker.Args, ctx)
		if err != nil {
			return mcpclient.MCPServerEntry{}, err
		}
		return mcpclient.MCPServerEntry{Command: "docker", Args: args, Env: env}, nil
	default:
		return mcpclient.MCPServerEntry{}, fmt.Errorf("unsupported strategy %q for mcp", strategy)
	}
}
```

- [ ] **Step 4: Run helper tests**

Run: `go test ./cmd/ -run TestRunMCPRegistration -v`
Expected: PASS.

- [ ] **Step 5: Wire into runInstall**

In `cmd/install.go` `runInstall`, after `runScriptFlow(...)` returns nil successfully and BEFORE `client.RecordInstall(...)`:
```go
// MCP client registration (only for type=mcp)
if pkg.Type == "mcp" {
	if err := runMCPRegistration(pkg, strategy, version, stdinReader, os.Stdout, installYes, installReconfigure); err != nil {
		fmt.Printf("⚠ MCP 클라이언트 자동 등록 실패: %v\n", err)
		fmt.Printf("  (수동 등록: 'claude mcp add-json %s ...' 또는 ~/.cursor/mcp.json / ~/.gemini/settings.json 편집)\n", pkg.MCPMetadata.ServerName)
	}
}
```

Add new flag at top of file:
```go
var installReconfigure bool
```

In `init()`:
```go
installCmd.Flags().BoolVar(&installReconfigure, "reconfigure", false, "MCP 패키지 재설치 시 env를 새로 입력")
```

- [ ] **Step 6: Run all cmd tests**

Run: `go test ./cmd/ -v`
Expected: PASS.

- [ ] **Step 7: Commit**
```bash
git add cmd/install.go cmd/install_mcp.go cmd/install_mcp_test.go
git commit -m "feat: runMCPRegistration hook in moso install"
```

---

### Task B10: Unregister hook in cmd/remove.go

**Files:**
- Modify: `cmd/remove.go`
- Modify: `cmd/remove_test.go` (or create)

- [ ] **Step 1: Read current remove flow**

Read `cmd/remove.go`. Locate the point where the main package is actually removed from the system (after deps resolution).

- [ ] **Step 2: Write failing test**

Add to `cmd/remove_test.go`:
```go
func TestUnregisterFromMCPClients_CallsAllDetected(t *testing.T) {
	pkg := &hub.Package{
		Type: "mcp", Slug: "gopedia-mcp",
		MCPMetadata: &hub.MCPMetadata{ServerName: "gopedia"},
	}
	a := &fakeClient{name: "a", detected: true}
	b := &fakeClient{name: "b", detected: false}
	unregisterFromClients(pkg, []mcpclient.MCPClient{a, b}, &bytes.Buffer{})
	assert.Equal(t, []string{"gopedia"}, a.unregistered)
	assert.Empty(t, b.unregistered)
}
```

- [ ] **Step 3: Run, expect FAIL**

Run: `go test ./cmd/ -run TestUnregisterFromMCPClients -v`
Expected: FAIL.

- [ ] **Step 4: Implement helper + wire**

In `cmd/remove.go` (or a new `cmd/remove_mcp.go` for symmetry with install):
```go
func unregisterFromClients(pkg *hub.Package, clients []mcpclient.MCPClient, out io.Writer) {
	if pkg.Type != "mcp" || pkg.MCPMetadata == nil {
		return
	}
	for _, c := range clients {
		if !c.Detected() {
			continue
		}
		if err := c.Unregister(pkg.MCPMetadata.ServerName); err != nil {
			fmt.Fprintf(out, "⚠ %s unregister 실패: %v\n", c.Name(), err)
			continue
		}
		fmt.Fprintf(out, "✓ %s에서 '%s' 제거\n", c.Name(), pkg.MCPMetadata.ServerName)
	}
}
```

In the runRemove function, after the package itself is removed successfully:
```go
if pkg.Type == "mcp" {
	unregisterFromClients(pkg, mcpclient.DetectInstalled(), os.Stdout)
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./cmd/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**
```bash
git add cmd/remove.go cmd/remove_test.go
git commit -m "feat: unregister from MCP clients on moso remove"
```

---

### Task B11: install.go fallback mcp+docker explicit error

**Files:**
- Modify: `cmd/install.go`

- [ ] **Step 1: Write failing test**

Add to `cmd/install_mcp_test.go`:
```go
func TestMCPDockerFallbackErrors(t *testing.T) {
	err := errorIfMCPFallbackUnsupported("mcp", "docker")
	assert.ErrorContains(t, err, "hub install script")
	assert.NoError(t, errorIfMCPFallbackUnsupported("mcp", "native"))
	assert.NoError(t, errorIfMCPFallbackUnsupported("npm", "docker"))
}
```

- [ ] **Step 2: Run, expect FAIL**

Run: `go test ./cmd/ -run TestMCPDockerFallback -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

In `cmd/install_mcp.go` add:
```go
func errorIfMCPFallbackUnsupported(pkgType, strategy string) error {
	if pkgType == "mcp" && strategy == "docker" {
		return fmt.Errorf("mcp 타입 + docker 전략은 hub install script가 필요합니다 (fallback 경로 미지원)")
	}
	return nil
}
```

In `cmd/install.go` runInstall, in the fallback branch (after `client.GetInstallScript` returns ErrNotFound, before `installer.BuildCommand`):
```go
if err := errorIfMCPFallbackUnsupported(string(pkg.Type), strategy); err != nil {
	return err
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./cmd/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**
```bash
git add cmd/install.go cmd/install_mcp.go cmd/install_mcp_test.go
git commit -m "feat: explicit error for mcp+docker fallback without hub script"
```

---

### Task B12: Full CLI test sweep + PR

- [ ] **Step 1: Run full suite**

```bash
go test ./...
go vet ./...
```
Expected: PASS, no vet warnings.

- [ ] **Step 2: Manual smoke against a local mock**

Build the CLI:
```bash
go build -o ./moso .
```

Set MOSO_DISABLE_CLAUDE_SUBPROCESS=1 to avoid touching the real claude CLI.
Point `~/.morphso/config.yaml` HubURL at a local dev hub.
Run: `./moso install gopedia-mcp --yes` (after Phase C publishes the package). For now: just confirm `./moso install --help` shows the new `--reconfigure` flag.

- [ ] **Step 3: Open PR for Phase B**

```bash
git push -u origin feat/mcp-package-type
gh pr create --title "feat: moso install supports mcp package type with auto-registration" --body "$(cat <<'EOF'
## Summary

- New `internal/mcpclient/` package with implementations for Claude Code (subprocess + direct edit), Cursor (mcpServers flat), Gemini CLI (mcp.servers nested)
- `runMCPRegistration` hook in `moso install` after install script success
- `moso remove` unregisters from detected clients
- `installer.go`: mcp split out of pip-fallthrough (now uses npm install -g)
- `RecommendForType` for mcp: prefers native, falls back to docker
- Token expansion: `{{image}}` `{{slug}}` `{{version}}`
- `--reconfigure` flag forces fresh env prompts on re-install
- Secret env values never echoed in any output

Depends on morphso-hub PR (mcp_metadata column + validation).

## Test plan

- [x] go test ./...
- [ ] E2E: register `moso install gopedia-mcp` against dev hub; verify Cursor / Gemini / Claude Code configs receive correct entries
- [ ] `moso remove gopedia-mcp` cleans up registrations
- [ ] Corrupt `~/.cursor/mcp.json` → install errors without overwriting
- [ ] Re-install reuses existing env values as defaults; `--reconfigure` forces re-prompt

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## Phase C — Data + E2E

### Task C1: Publish gopedia-mcp package via authenticated POST

**Files:** none (manual ops task).

- [ ] **Step 1: Verify Phase A merged + deployed**

```bash
curl -s $HUB_URL/health
```
Expected: 200.

Optional schema check from a hub pod:
```bash
kubectl -n morphso-hub exec deploy/morphso-hub -- psql $DB_URL -c "\d packages" | grep mcp_metadata
```
Expected: `mcp_metadata | jsonb`.

- [ ] **Step 2: Login + create package**

Using `moso login` (or directly with token):
```bash
moso login
TOKEN=$(cat ~/.morphso/config.yaml | grep '^token:' | awk '{print $2}')
curl -s -X POST $HUB_URL/packages \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d @- <<'EOF'
{
  "name": "Gopedia MCP Server",
  "slug": "gopedia-mcp",
  "description": "Gopedia HTTP API를 MCP 도구로 노출하는 stdio 서버",
  "type": "mcp",
  "recommended_strategy": "native",
  "tags": ["mcp", "gopedia", "search"],
  "mcp_metadata": {
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
    "env_schema": [
      {
        "name": "GOPEDIA_HOST_DOMAIN",
        "prompt": "Gopedia API host (예: 127.0.0.1:18787)",
        "required": true,
        "default": "127.0.0.1:18787"
      }
    ]
  }
}
EOF
```
Expected: 201, response includes `mcp_metadata`.

- [ ] **Step 3: Register required dep on gopedia**

(Per existing dependency PUT endpoint — match Phase A patterns):
```bash
curl -X PUT $HUB_URL/packages/gopedia-mcp/dependencies \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"dependencies":[{"slug":"gopedia","min_version":"","optional":false}]}'
```

- [ ] **Step 4: Generate install scripts for native + docker**

(Either via existing hub admin endpoint that triggers LLM generation, or hand-author + POST. Match existing process — read the install_scripts handler.)

For native:
```sh
#!/bin/sh
[ -n "$MOSO_CONFIG" ] && . "$MOSO_CONFIG"
npm install -g gopedia-mcp-server@${VERSION:-latest}
```

For docker:
```sh
#!/bin/sh
[ -n "$MOSO_CONFIG" ] && . "$MOSO_CONFIG"
docker pull artifacts.toji.homes/gopedia-mcp:${VERSION:-latest}
```

Submit via the install-script endpoint. Hub validation rejects scripts with forbidden patterns.

- [ ] **Step 5: Verify via GET**
```bash
curl -s $HUB_URL/packages/gopedia-mcp | jq '.mcp_metadata.server_name'
```
Expected: `"gopedia"`.

---

### Task C2: E2E install on clean machine

- [ ] **Step 1: Build latest CLI**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso-wt-mcp-type
go build -o /tmp/moso .
```

- [ ] **Step 2: Run install**

```bash
/tmp/moso login
/tmp/moso install gopedia-mcp
```

Expected interactive flow:
- gopedia required dep prompt → install
- "Gopedia API host [기본: 127.0.0.1:18787]:" prompt
- "✓ claude-code에 'gopedia' 등록" (or `· skip` if undetected)
- "✓ cursor에 'gopedia' 등록" (or skip)
- "✓ gemini-cli에 'gopedia' 등록" (or skip)

- [ ] **Step 3: Verify registration in each client**

```bash
# Claude Code
claude mcp list | grep gopedia
# or
jq '.mcpServers.gopedia' ~/.claude.json

# Cursor
jq '.mcpServers.gopedia' ~/.cursor/mcp.json

# Gemini CLI
jq '.mcp.servers.gopedia' ~/.gemini/settings.json
```

All should show `command: "gopedia-mcp-server"` and `env.GOPEDIA_HOST_DOMAIN`.

- [ ] **Step 4: Verify actual MCP tool call works**

In Claude Code: `/mcp` → confirm gopedia connected. Call `mcp__gopedia__gopedia_health` (or equivalent).
Expected: server responds.

- [ ] **Step 5: Test removal**

```bash
/tmp/moso remove gopedia-mcp
```
Expected: "✓ claude-code에서 'gopedia' 제거" etc. Re-check each config — entry gone.

- [ ] **Step 6: Test idempotent reinstall**

```bash
/tmp/moso install gopedia-mcp
```
At env prompt: just hit Enter — should reuse default. (If a prior install left state.)

Then:
```bash
/tmp/moso install gopedia-mcp --reconfigure
```
Should prompt fresh.

---

## Self-Review Notes

Coverage map (spec section → task):
- Hub schema → A1
- MCPMetadata domain types → A2
- Store persistence → A3
- API validation rules (regex, token namespace, embedded env reject) → A4
- LLM prompt mcp branch + forbidden-pattern post-validation → A5
- hub client types → B1
- mcpclient interface + AllClients/DetectInstalled → B2
- Cursor `mcpServers` flat → B3
- Gemini CLI `mcp.servers` nested → B4
- Claude Code subprocess + direct edit fallback → B5
- installer.go mcp split → B6
- LocalRecommend mcp branch → B7
- Token expansion → B8
- runMCPRegistration + windows skip + transport check + idempotency via ExistingEntry + --reconfigure + secret masking → B9
- Unregister on remove → B10
- Explicit mcp+docker fallback error → B11
- Package publish + install scripts → C1
- E2E verification → C2

Known caveats called out by spec:
- Windows registration not implemented (handled in B9, tested).
- HTTP/SSE transport not implemented (handled in B9, tested).
- `last-writer-wins` concurrency: no locking introduced (consistent with spec).
- `pip/recipe/binary/helm` keeps pip fallthrough — out of scope, called out in B6.
- `RecordInstall` schema unchanged (registered_clients deferred to v2 per spec).
