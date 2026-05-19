# morphso CLI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** morphso CLI — Go 단일 바이너리, OS 스펙 수집 + 캐시, ZITADEL Device Flow 인증, morphso-hub REST API 연동, install/search/list/login 커맨드 구현.

**Architecture:** cobra 기반 CLI. `internal/` 패키지로 config, spec, hub client, auth, installer를 분리. `cmd/` 패키지에서 cobra 커맨드 정의. 설치 추천은 token 있으면 hub `/recommend` 호출, 없으면 클라이언트 측 rule-based 폴백.

**Tech Stack:** Go 1.23, cobra v1, yaml.v3, 표준 라이브러리(net/http, os/exec, runtime)

---

## 파일 구조

```
morphso/
  main.go                            # cobra root 실행 진입점
  go.mod                             # module github.com/tojiuni/morphso
  cmd/
    root.go                          # 루트 커맨드, 전역 플래그 (--hub-url, --no-color)
    login.go                         # morphso login  (ZITADEL Device Flow)
    logout.go                        # morphso logout
    whoami.go                        # morphso whoami
    search.go                        # morphso search <query>
    info.go                          # morphso info <package>
    install.go                       # morphso install <package> [--strategy=...] [--yes]
    list.go                          # morphso list
    remove.go                        # morphso remove (stub: "not yet implemented")
    publish.go                       # morphso publish (stub: "not yet implemented")
  internal/
    config/
      config.go                      # ~/.morphso/config.yaml 로드/저장
      config_test.go
    spec/
      spec.go                        # OS 스펙 수집 + ~/.morphso/spec.yaml 캐시 (TTL 24h)
      spec_test.go
    hub/
      client.go                      # morphso-hub REST API 클라이언트
      types.go                       # hub API 요청/응답 타입 정의
      client_test.go
    auth/
      device_flow.go                 # ZITADEL Device Flow (poll-based)
      device_flow_test.go
    installer/
      installer.go                   # install plan 실행 (native/docker/k8s/helm)
      installer_test.go
```

---

## Task 1: 프로젝트 부트스트랩

**Files:**
- Create: `go.mod`
- Create: `main.go`
- Create: `cmd/root.go`

- [ ] **Step 1: go.mod 및 의존성 설치**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
go mod init github.com/tojiuni/morphso
go get github.com/spf13/cobra@v1.8.1
go get gopkg.in/yaml.v3@v3.0.1
```

- [ ] **Step 2: `main.go` 작성**

```go
package main

import "github.com/tojiuni/morphso/cmd"

func main() {
	cmd.Execute()
}
```

- [ ] **Step 3: `cmd/root.go` 작성**

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	hubURL string
)

var rootCmd = &cobra.Command{
	Use:   "morphso",
	Short: "AI service marketplace CLI",
	Long:  "morphso — AI 전용 서비스 마켓플레이스 CLI. 패키지를 검색하고 AI 추천 방식으로 설치합니다.",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&hubURL, "hub-url", "", "morphso-hub URL override (기본값: config.yaml의 hub_url)")
}
```

- [ ] **Step 4: 빌드 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
go build ./...
```
Expected: 오류 없음

- [ ] **Step 5: 커밋**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
git add go.mod go.sum main.go cmd/root.go
git commit -m "feat: bootstrap morphso CLI project"
git push origin main
```

---

## Task 2: Config 관리

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/config/config_test.go`:

```go
package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tojiuni/morphso/internal/config"
)

func TestConfig_DefaultHubURL(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "https://morphso.toji.homes", cfg.HubURL)
}

func TestConfig_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	require.NoError(t, err)

	cfg.Token = "test-token-abc"
	cfg.HubURL = "https://custom.hub.io"
	require.NoError(t, cfg.Save())

	loaded, err := config.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "test-token-abc", loaded.Token)
	assert.Equal(t, "https://custom.hub.io", loaded.HubURL)
}

func TestConfig_ClearToken(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	require.NoError(t, err)
	cfg.Token = "some-token"
	require.NoError(t, cfg.Save())

	cfg.Token = ""
	require.NoError(t, cfg.Save())

	loaded, err := config.Load(dir)
	require.NoError(t, err)
	assert.Empty(t, loaded.Token)
}

func TestConfig_Dir(t *testing.T) {
	dir := t.TempDir()
	cfg, _ := config.Load(dir)
	assert.Equal(t, filepath.Join(dir, "config.yaml"), cfg.Path())
}
```

- [ ] **Step 2: 테스트 실패 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
go get github.com/stretchr/testify@v1.10.0
go test ./internal/config/... -v 2>&1 | head -10
```
Expected: compile error — `config.Load` undefined

- [ ] **Step 3: `internal/config/config.go` 구현**

```go
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const defaultHubURL = "https://morphso.toji.homes"

type Config struct {
	HubURL string `yaml:"hub_url"`
	Token  string `yaml:"token"`
	dir    string
}

// Load reads ~/.morphso/config.yaml (or dir/config.yaml in tests).
// Returns a Config with defaults if the file doesn't exist.
func Load(dir string) (*Config, error) {
	cfg := &Config{
		HubURL: defaultHubURL,
		dir:    dir,
	}
	path := cfg.Path()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.dir = dir
	if cfg.HubURL == "" {
		cfg.HubURL = defaultHubURL
	}
	return cfg, nil
}

// DefaultLoad loads from ~/.morphso/config.yaml.
func DefaultLoad() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home dir: %w", err)
	}
	return Load(filepath.Join(home, ".morphso"))
}

func (c *Config) Path() string {
	return filepath.Join(c.dir, "config.yaml")
}

func (c *Config) Save() error {
	if err := os.MkdirAll(c.dir, 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(c.Path(), data, 0600)
}
```

- [ ] **Step 4: 테스트 통과 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
go test ./internal/config/... -v -race
```
Expected: 4개 PASS

- [ ] **Step 5: 커밋**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
git add internal/config/
git commit -m "feat: add config management (~/.morphso/config.yaml)"
git push origin main
```

---

## Task 3: OS 스펙 수집 + 캐시

**Files:**
- Create: `internal/spec/spec.go`
- Create: `internal/spec/spec_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/spec/spec_test.go`:

```go
package spec_test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tojiuni/morphso/internal/spec"
)

func TestCollect_PopulatesOSAndArch(t *testing.T) {
	s, err := spec.Collect()
	require.NoError(t, err)
	assert.NotEmpty(t, s.OS)
	assert.NotEmpty(t, s.Arch)
	assert.Greater(t, s.MemoryTotalGB, 0.0)
	assert.Greater(t, s.DiskTotalGB, 0.0)
}

func TestCollect_InstalledTools(t *testing.T) {
	s, err := spec.Collect()
	require.NoError(t, err)
	// InstalledTools map은 초기화되어 있어야 함 (nil이 아님)
	assert.NotNil(t, s.InstalledTools)
}

func TestCache_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	s := &spec.Spec{
		CollectedAt:   time.Now(),
		OS:            "darwin",
		Arch:          "arm64",
		OSVersion:     "15.2",
		MemoryTotalGB: 32,
		MemoryFreeGB:  18,
		DiskTotalGB:   500,
		DiskFreeGB:    200,
		InstalledTools: map[string]string{
			"docker": "27.1.0",
		},
	}
	require.NoError(t, spec.SaveCache(dir, s))

	loaded, err := spec.LoadCache(dir)
	require.NoError(t, err)
	assert.Equal(t, "darwin", loaded.OS)
	assert.Equal(t, "27.1.0", loaded.InstalledTools["docker"])
}

func TestCache_ExpiredReturnsNil(t *testing.T) {
	dir := t.TempDir()
	s := &spec.Spec{
		CollectedAt: time.Now().Add(-25 * time.Hour), // 24h TTL 초과
		OS:          "linux",
	}
	require.NoError(t, spec.SaveCache(dir, s))

	loaded, err := spec.LoadCache(dir)
	require.NoError(t, err)
	assert.Nil(t, loaded) // expired → nil
}

func TestCache_MissingReturnsNil(t *testing.T) {
	dir := t.TempDir()
	// 파일 없음
	loaded, err := spec.LoadCache(dir)
	require.NoError(t, err)
	assert.Nil(t, loaded)
}

func TestGetOrCollect_UsesCacheIfFresh(t *testing.T) {
	dir := t.TempDir()
	cached := &spec.Spec{
		CollectedAt:   time.Now(),
		OS:            "linux",
		Arch:          "amd64",
		MemoryTotalGB: 8,
		DiskTotalGB:   100,
		InstalledTools: map[string]string{},
	}
	require.NoError(t, spec.SaveCache(dir, cached))

	s, err := spec.GetOrCollect(dir)
	require.NoError(t, err)
	assert.Equal(t, "linux", s.OS)
}
```

- [ ] **Step 2: 테스트 실패 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
go test ./internal/spec/... -v 2>&1 | head -10
```
Expected: compile error — `spec.Collect` undefined

- [ ] **Step 3: `internal/spec/spec.go` 구현**

```go
package spec

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const cacheTTL = 24 * time.Hour

type Spec struct {
	CollectedAt    time.Time         `yaml:"collected_at"`
	OS             string            `yaml:"os"`
	Arch           string            `yaml:"arch"`
	OSVersion      string            `yaml:"os_version"`
	MemoryTotalGB  float64           `yaml:"memory_total_gb"`
	MemoryFreeGB   float64           `yaml:"memory_free_gb"`
	DiskTotalGB    float64           `yaml:"disk_total_gb"`
	DiskFreeGB     float64           `yaml:"disk_free_gb"`
	GPU            *string           `yaml:"gpu"`
	InstalledTools map[string]string `yaml:"installed_tools"`
}

// Collect gathers current OS spec from the system.
func Collect() (*Spec, error) {
	s := &Spec{
		CollectedAt:    time.Now(),
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		InstalledTools: make(map[string]string),
	}

	s.OSVersion = collectOSVersion()
	s.MemoryTotalGB, s.MemoryFreeGB = collectMemory()
	s.DiskTotalGB, s.DiskFreeGB = collectDisk()

	tools := []string{"docker", "kubectl", "helm", "pip", "pip3", "npm", "brew"}
	for _, tool := range tools {
		if v := toolVersion(tool); v != "" {
			s.InstalledTools[tool] = v
		}
	}
	// normalize: prefer pip3 as "pip"
	if _, ok := s.InstalledTools["pip"]; !ok {
		if v, ok := s.InstalledTools["pip3"]; ok {
			s.InstalledTools["pip"] = v
		}
	}
	delete(s.InstalledTools, "pip3")

	return s, nil
}

func collectOSVersion() string {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("sw_vers", "-productVersion").Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	case "linux":
		data, err := os.ReadFile("/etc/os-release")
		if err != nil {
			return ""
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "VERSION_ID=") {
				return strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), `"`)
			}
		}
	}
	return ""
}

func collectMemory() (total, free float64) {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
		if err == nil {
			if b, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64); err == nil {
				total = float64(b) / (1024 * 1024 * 1024)
			}
		}
		// vm_stat for free pages
		out, err = exec.Command("vm_stat").Output()
		if err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				if strings.HasPrefix(line, "Pages free:") {
					parts := strings.Fields(line)
					if len(parts) >= 3 {
						if pages, err := strconv.ParseInt(strings.TrimRight(parts[2], "."), 10, 64); err == nil {
							free = float64(pages*4096) / (1024 * 1024 * 1024)
						}
					}
				}
			}
		}
	case "linux":
		data, err := os.ReadFile("/proc/meminfo")
		if err != nil {
			return
		}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			kb, _ := strconv.ParseInt(fields[1], 10, 64)
			switch {
			case strings.HasPrefix(line, "MemTotal:"):
				total = float64(kb) / (1024 * 1024)
			case strings.HasPrefix(line, "MemAvailable:"):
				free = float64(kb) / (1024 * 1024)
			}
		}
	}
	return
}

func collectDisk() (total, free float64) {
	out, err := exec.Command("df", "-k", "/").Output()
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 4 {
		return
	}
	if t, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
		total = float64(t) / (1024 * 1024)
	}
	if f, err := strconv.ParseInt(fields[3], 10, 64); err == nil {
		free = float64(f) / (1024 * 1024)
	}
	return
}

func toolVersion(tool string) string {
	var out bytes.Buffer
	cmd := exec.Command(tool, "--version")
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return ""
	}
	line := strings.SplitN(strings.TrimSpace(out.String()), "\n", 2)[0]
	// extract version number: "Docker version 27.1.0, ..." → "27.1.0"
	fields := strings.Fields(line)
	for _, f := range fields {
		f = strings.TrimRight(f, ",.")
		if len(f) > 0 && (f[0] >= '0' && f[0] <= '9') {
			return f
		}
	}
	return line
}

func cachePath(dir string) string {
	return filepath.Join(dir, "spec.yaml")
}

// SaveCache writes the spec to dir/spec.yaml.
func SaveCache(dir string, s *Spec) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create spec dir: %w", err)
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal spec: %w", err)
	}
	return os.WriteFile(cachePath(dir), data, 0600)
}

// LoadCache reads dir/spec.yaml. Returns nil (no error) if missing or expired.
func LoadCache(dir string) (*Spec, error) {
	data, err := os.ReadFile(cachePath(dir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read spec cache: %w", err)
	}
	var s Spec
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse spec cache: %w", err)
	}
	if time.Since(s.CollectedAt) > cacheTTL {
		return nil, nil
	}
	return &s, nil
}

// GetOrCollect returns a fresh spec from cache or by collecting.
func GetOrCollect(dir string) (*Spec, error) {
	if s, err := LoadCache(dir); err == nil && s != nil {
		return s, nil
	}
	s, err := Collect()
	if err != nil {
		return nil, err
	}
	_ = SaveCache(dir, s)
	return s, nil
}
```

- [ ] **Step 4: 테스트 통과 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
go test ./internal/spec/... -v -race
```
Expected: 6개 PASS

- [ ] **Step 5: 커밋**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
git add internal/spec/
git commit -m "feat: add OS spec collection and 24h cache"
git push origin main
```

---

## Task 4: Hub API 클라이언트

**Files:**
- Create: `internal/hub/types.go`
- Create: `internal/hub/client.go`
- Create: `internal/hub/client_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/hub/client_test.go`:

```go
package hub_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tojiuni/morphso/internal/hub"
	"github.com/tojiuni/morphso/internal/spec"
)

func TestClient_Search(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/packages", r.URL.Path)
		assert.Equal(t, "gopedia", r.URL.Query().Get("q"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]hub.Package{
			{Slug: "gopedia", Name: "Gopedia", Type: "mcp"},
		})
	}))
	defer srv.Close()

	client := hub.NewClient(srv.URL, "")
	pkgs, err := client.Search("gopedia", 10, 0)
	require.NoError(t, err)
	require.Len(t, pkgs, 1)
	assert.Equal(t, "gopedia", pkgs[0].Slug)
}

func TestClient_GetPackage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/packages/gopedia", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(hub.Package{Slug: "gopedia", Type: "mcp", Verified: true})
	}))
	defer srv.Close()

	client := hub.NewClient(srv.URL, "")
	pkg, err := client.GetPackage("gopedia")
	require.NoError(t, err)
	assert.Equal(t, "mcp", pkg.Type)
}

func TestClient_GetPackage_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	client := hub.NewClient(srv.URL, "")
	_, err := client.GetPackage("unknown")
	assert.ErrorIs(t, err, hub.ErrNotFound)
}

func TestClient_Recommend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/recommend", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		// hub의 recommend.Result는 json 태그 없음 → 대문자
		json.NewEncoder(w).Encode(map[string]string{
			"Strategy": "k8s",
			"Reason":   "kubectl detected",
		})
	}))
	defer srv.Close()

	client := hub.NewClient(srv.URL, "test-token")
	s := &spec.Spec{
		OS: "darwin", Arch: "arm64", MemoryFreeGB: 18,
		InstalledTools: map[string]string{"kubectl": "1.35.0", "helm": "3.15.0"},
	}
	result, err := client.Recommend("gopedia", s, "")
	require.NoError(t, err)
	assert.Equal(t, "k8s", result.Strategy)
	assert.Equal(t, "kubectl detected", result.Reason)
}

func TestClient_RecordInstall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/installs", r.URL.Path)
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := hub.NewClient(srv.URL, "tok")
	err := client.RecordInstall("gopedia", "1.0.0", "native")
	require.NoError(t, err)
}

func TestClient_GetInstalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/users/me/installs", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]hub.InstallRecord{
			{PackageSlug: "gopedia", Strategy: "k8s", InstalledAt: time.Now()},
		})
	}))
	defer srv.Close()

	client := hub.NewClient(srv.URL, "tok")
	records, err := client.GetInstalls()
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "gopedia", records[0].PackageSlug)
}
```

- [ ] **Step 2: 테스트 실패 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
go test ./internal/hub/... -v 2>&1 | head -10
```
Expected: compile error

- [ ] **Step 3: `internal/hub/types.go` 작성**

```go
package hub

import (
	"errors"
	"time"
)

var ErrNotFound = errors.New("package not found")
var ErrUnauthorized = errors.New("unauthorized")

type Package struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Slug        string   `json:"slug"`
	Description string   `json:"description"`
	Type        string   `json:"type"`       // pip|npm|binary|mcp|recipe|helm
	Verified    bool     `json:"verified"`
	PriceCents  int64    `json:"price_cents"`
	Downloads   int64    `json:"downloads"`
	Tags        []string `json:"tags"`
}

// RecommendResponse mirrors morphso-hub's recommend.Result (no json tags → uppercase).
type RecommendResponse struct {
	Strategy string `json:"Strategy"`
	Reason   string `json:"Reason"`
}

type InstallRequest struct {
	PackageSlug string `json:"package_slug"`
	Version     string `json:"version"`
	Strategy    string `json:"strategy"`
}

type InstallRecord struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	PackageSlug string    `json:"package_slug"`
	Version     string    `json:"version"`
	Strategy    string    `json:"strategy"`
	OS          string    `json:"os"`
	Arch        string    `json:"arch"`
	InstalledAt time.Time `json:"installed_at"`
}
```

- [ ] **Step 4: `internal/hub/client.go` 작성**

```go
package hub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"runtime"

	"github.com/tojiuni/morphso/internal/spec"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{},
	}
}

func (c *Client) newRequest(method, path string, body any) (*http.Request, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, fmt.Errorf("encode body: %w", err)
		}
	}
	req, err := http.NewRequest(method, c.baseURL+path, &buf)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return req, nil
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusNoContent:
		return nil
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("server error: %s", resp.Status)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

func (c *Client) Search(query string, limit, offset int) ([]Package, error) {
	u := fmt.Sprintf("/packages?q=%s&limit=%d&offset=%d",
		url.QueryEscape(query), limit, offset)
	req, err := c.newRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	var pkgs []Package
	if err := c.do(req, &pkgs); err != nil {
		return nil, err
	}
	if pkgs == nil {
		pkgs = []Package{}
	}
	return pkgs, nil
}

func (c *Client) GetPackage(slug string) (*Package, error) {
	req, err := c.newRequest(http.MethodGet, "/packages/"+slug, nil)
	if err != nil {
		return nil, err
	}
	var pkg Package
	if err := c.do(req, &pkg); err != nil {
		return nil, err
	}
	return &pkg, nil
}

type recommendBody struct {
	PackageSlug       string    `json:"package_slug"`
	Spec              *spec.Spec `json:"spec"`
	PreferredStrategy *string   `json:"preferred_strategy"`
}

func (c *Client) Recommend(slug string, s *spec.Spec, preferred string) (*RecommendResponse, error) {
	body := recommendBody{PackageSlug: slug, Spec: s}
	if preferred != "" {
		body.PreferredStrategy = &preferred
	}
	req, err := c.newRequest(http.MethodPost, "/recommend", body)
	if err != nil {
		return nil, err
	}
	var result RecommendResponse
	if err := c.do(req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) RecordInstall(slug, version, strategy string) error {
	body := InstallRequest{
		PackageSlug: slug,
		Version:     version,
		Strategy:    strategy,
	}
	req, err := c.newRequest(http.MethodPost, "/installs", body)
	if err != nil {
		return err
	}
	return c.do(req, nil)
}

func (c *Client) GetInstalls() ([]InstallRecord, error) {
	req, err := c.newRequest(http.MethodGet, "/users/me/installs", nil)
	if err != nil {
		return nil, err
	}
	var records []InstallRecord
	if err := c.do(req, &records); err != nil {
		return nil, err
	}
	if records == nil {
		records = []InstallRecord{}
	}
	return records, nil
}

// LocalRecommend applies the same rule-based logic as morphso-hub for offline use.
func LocalRecommend(s *spec.Spec, preferred string) string {
	if preferred != "" {
		return preferred
	}
	_, hasKubectl := s.InstalledTools["kubectl"]
	_, hasHelm := s.InstalledTools["helm"]
	_, hasDocker := s.InstalledTools["docker"]

	if hasKubectl && hasHelm && s.MemoryFreeGB >= 4 {
		return "k8s"
	}
	if hasDocker && s.MemoryFreeGB >= 2 {
		return "docker"
	}
	return "native"
}

// OS returns GOOS for use in install records.
func OS() string { return runtime.GOOS }

// Arch returns GOARCH for use in install records.
func Arch() string { return runtime.GOARCH }
```

- [ ] **Step 5: 테스트 통과 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
go test ./internal/hub/... -v -race
```
Expected: 5개 PASS

- [ ] **Step 6: 커밋**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
git add internal/hub/
git commit -m "feat: add morphso-hub REST API client"
git push origin main
```

---

## Task 5: Auth (ZITADEL Device Flow + login/logout/whoami)

**Files:**
- Create: `internal/auth/device_flow.go`
- Create: `internal/auth/device_flow_test.go`
- Create: `cmd/login.go`
- Create: `cmd/logout.go`
- Create: `cmd/whoami.go`

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/auth/device_flow_test.go`:

```go
package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tojiuni/morphso/internal/auth"
)

func TestDeviceFlow_Start(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/oauth/v2/device_authorization", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "dev_abc",
			"user_code":        "ABCD-1234",
			"verification_uri": "https://auth.example.com/activate",
			"expires_in":       300,
			"interval":         5,
		})
	}))
	defer srv.Close()

	flow := auth.NewDeviceFlow(srv.URL, "morphso-cli")
	resp, err := flow.Start()
	require.NoError(t, err)
	assert.Equal(t, "dev_abc", resp.DeviceCode)
	assert.Equal(t, "ABCD-1234", resp.UserCode)
	assert.Equal(t, "https://auth.example.com/activate", resp.VerificationURI)
}

func TestDeviceFlow_Poll_Success(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 2 {
			// 첫 번째 폴링: authorization_pending
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
			return
		}
		// 두 번째 폴링: 성공
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"access_token": "tok_xyz"})
	}))
	defer srv.Close()

	flow := auth.NewDeviceFlow(srv.URL, "morphso-cli")
	token, err := flow.PollOnce("dev_code")
	require.NoError(t, err)
	assert.Equal(t, auth.ErrAuthorizationPending, err) // first call → pending
	_ = token

	token, err = flow.PollOnce("dev_code")
	require.NoError(t, err)
	assert.Equal(t, "tok_xyz", token)
}
```

**Note**: `TestDeviceFlow_Poll_Success`에서 첫 번째 `PollOnce` 호출은 `ErrAuthorizationPending`을 반환해야 하므로 두 테스트를 나눠서 작성.

`internal/auth/device_flow_test.go` (수정):

```go
package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tojiuni/morphso/internal/auth"
)

func TestDeviceFlow_Start(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/oauth/v2/device_authorization", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "dev_abc",
			"user_code":        "ABCD-1234",
			"verification_uri": "https://auth.example.com/activate",
			"expires_in":       300,
			"interval":         5,
		})
	}))
	defer srv.Close()

	flow := auth.NewDeviceFlow(srv.URL, "morphso-cli")
	resp, err := flow.Start()
	require.NoError(t, err)
	assert.Equal(t, "dev_abc", resp.DeviceCode)
	assert.Equal(t, "ABCD-1234", resp.UserCode)
}

func TestDeviceFlow_PollOnce_Pending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
	}))
	defer srv.Close()

	flow := auth.NewDeviceFlow(srv.URL, "morphso-cli")
	_, err := flow.PollOnce("dev_code")
	assert.ErrorIs(t, err, auth.ErrAuthorizationPending)
}

func TestDeviceFlow_PollOnce_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/oauth/v2/token", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"access_token": "tok_xyz"})
	}))
	defer srv.Close()

	flow := auth.NewDeviceFlow(srv.URL, "morphso-cli")
	token, err := flow.PollOnce("dev_code")
	require.NoError(t, err)
	assert.Equal(t, "tok_xyz", token)
}
```

- [ ] **Step 2: 테스트 실패 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
go test ./internal/auth/... -v 2>&1 | head -10
```
Expected: compile error

- [ ] **Step 3: `internal/auth/device_flow.go` 구현**

```go
package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

var (
	ErrAuthorizationPending = errors.New("authorization pending")
	ErrSlowDown             = errors.New("slow down")
	ErrAccessDenied         = errors.New("access denied")
	ErrExpired              = errors.New("device code expired")
)

const defaultClientID = "morphso-cli"

type DeviceFlow struct {
	issuer   string
	clientID string
	http     *http.Client
}

func NewDeviceFlow(issuer, clientID string) *DeviceFlow {
	if clientID == "" {
		clientID = defaultClientID
	}
	return &DeviceFlow{issuer: issuer, clientID: clientID, http: &http.Client{}}
}

type StartResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

func (f *DeviceFlow) Start() (*StartResponse, error) {
	body := url.Values{
		"client_id": {f.clientID},
		"scope":     {"openid profile email"},
	}
	resp, err := f.http.Post(
		f.issuer+"/oauth/v2/device_authorization",
		"application/x-www-form-urlencoded",
		strings.NewReader(body.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("device authorization: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device authorization failed: %s", resp.Status)
	}
	var result StartResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode device response: %w", err)
	}
	return &result, nil
}

// PollOnce makes one token poll attempt.
// Returns (token, nil) on success, (_, ErrAuthorizationPending) while waiting,
// or another error if the flow fails permanently.
func (f *DeviceFlow) PollOnce(deviceCode string) (string, error) {
	body := url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"client_id":   {f.clientID},
		"device_code": {deviceCode},
	}
	resp, err := f.http.Post(
		f.issuer+"/oauth/v2/token",
		"application/x-www-form-urlencoded",
		strings.NewReader(body.Encode()),
	)
	if err != nil {
		return "", fmt.Errorf("token poll: %w", err)
	}
	defer resp.Body.Close()

	var payload struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}

	if payload.AccessToken != "" {
		return payload.AccessToken, nil
	}

	switch payload.Error {
	case "authorization_pending":
		return "", ErrAuthorizationPending
	case "slow_down":
		return "", ErrSlowDown
	case "access_denied":
		return "", ErrAccessDenied
	case "expired_token":
		return "", ErrExpired
	default:
		return "", fmt.Errorf("token error: %s", payload.Error)
	}
}
```

- [ ] **Step 4: 테스트 통과 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
go test ./internal/auth/... -v -race
```
Expected: 3개 PASS

- [ ] **Step 5: `cmd/login.go` 작성**

```go
package cmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/tojiuni/morphso/internal/auth"
	"github.com/tojiuni/morphso/internal/config"
)

const zitadelIssuer = "https://auth.toji.homes"

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "ZITADEL Device Flow로 로그인",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.DefaultLoad()
		if err != nil {
			return err
		}

		flow := auth.NewDeviceFlow(zitadelIssuer, "morphso-cli")
		resp, err := flow.Start()
		if err != nil {
			return fmt.Errorf("인증 시작 실패: %w", err)
		}

		fmt.Printf("\n브라우저에서 다음 URL로 이동하세요:\n  %s\n", resp.VerificationURI)
		fmt.Printf("\n코드를 입력하세요: %s\n\n", resp.UserCode)
		fmt.Print("인증 완료를 기다리는 중")

		interval := time.Duration(resp.Interval) * time.Second
		if interval == 0 {
			interval = 5 * time.Second
		}
		deadline := time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)

		for time.Now().Before(deadline) {
			time.Sleep(interval)
			fmt.Print(".")

			token, err := flow.PollOnce(resp.DeviceCode)
			if errors.Is(err, auth.ErrAuthorizationPending) || errors.Is(err, auth.ErrSlowDown) {
				continue
			}
			if err != nil {
				return fmt.Errorf("\n인증 실패: %w", err)
			}

			cfg.Token = token
			if err := cfg.Save(); err != nil {
				return fmt.Errorf("토큰 저장 실패: %w", err)
			}
			fmt.Println("\n\n✓ 로그인 성공!")
			return nil
		}
		return fmt.Errorf("인증 시간 초과")
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
}
```

- [ ] **Step 6: `cmd/logout.go` 작성**

```go
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tojiuni/morphso/internal/config"
)

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "로그아웃 (토큰 삭제)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.DefaultLoad()
		if err != nil {
			return err
		}
		cfg.Token = ""
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Println("로그아웃 완료.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(logoutCmd)
}
```

- [ ] **Step 7: `cmd/whoami.go` 작성**

```go
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tojiuni/morphso/internal/config"
)

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "현재 로그인 상태 표시",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.DefaultLoad()
		if err != nil {
			return err
		}
		if cfg.Token == "" {
			fmt.Println("로그인되지 않음. 'morphso login'으로 로그인하세요.")
			return nil
		}
		fmt.Println("로그인됨 (토큰 있음). 자세한 정보: morphso-hub /users/me 미구현.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(whoamiCmd)
}
```

- [ ] **Step 8: 빌드 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
go build ./...
go test ./internal/auth/... -v -race
```
Expected: 빌드 성공, 3개 PASS

- [ ] **Step 9: 커밋**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
git add internal/auth/ cmd/login.go cmd/logout.go cmd/whoami.go
git commit -m "feat: add ZITADEL Device Flow auth and login/logout/whoami commands"
git push origin main
```

---

## Task 6: search + info 커맨드

**Files:**
- Create: `cmd/search.go`
- Create: `cmd/info.go`

- [ ] **Step 1: `cmd/search.go` 작성**

```go
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tojiuni/morphso/internal/config"
	"github.com/tojiuni/morphso/internal/hub"
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "패키지 검색",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.DefaultLoad()
		if err != nil {
			return err
		}
		if hubURL != "" {
			cfg.HubURL = hubURL
		}
		client := hub.NewClient(cfg.HubURL, cfg.Token)
		pkgs, err := client.Search(args[0], 20, 0)
		if err != nil {
			return fmt.Errorf("검색 실패: %w", err)
		}
		if len(pkgs) == 0 {
			fmt.Printf("'%s'에 대한 결과 없음\n", args[0])
			return nil
		}
		fmt.Printf("%-20s %-8s %-6s %s\n", "NAME", "TYPE", "PRICE", "DESCRIPTION")
		fmt.Println("------------------------------------------------------------")
		for _, p := range pkgs {
			price := "무료"
			if p.PriceCents > 0 {
				price = fmt.Sprintf("₩%d", p.PriceCents)
			}
			verified := ""
			if p.Verified {
				verified = " ✓"
			}
			fmt.Printf("%-20s %-8s %-6s %s%s\n", p.Slug, p.Type, price, p.Description, verified)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(searchCmd)
}
```

- [ ] **Step 2: `cmd/info.go` 작성**

```go
package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tojiuni/morphso/internal/config"
	"github.com/tojiuni/morphso/internal/hub"
)

var infoCmd = &cobra.Command{
	Use:   "info <package>",
	Short: "패키지 상세 정보",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.DefaultLoad()
		if err != nil {
			return err
		}
		if hubURL != "" {
			cfg.HubURL = hubURL
		}
		client := hub.NewClient(cfg.HubURL, cfg.Token)
		pkg, err := client.GetPackage(args[0])
		if errors.Is(err, hub.ErrNotFound) {
			return fmt.Errorf("패키지 '%s'를 찾을 수 없습니다", args[0])
		}
		if err != nil {
			return fmt.Errorf("정보 조회 실패: %w", err)
		}

		verified := ""
		if pkg.Verified {
			verified = " [검증됨]"
		}
		price := "무료"
		if pkg.PriceCents > 0 {
			price = fmt.Sprintf("₩%d", pkg.PriceCents)
		}

		fmt.Printf("이름:       %s%s\n", pkg.Name, verified)
		fmt.Printf("Slug:       %s\n", pkg.Slug)
		fmt.Printf("타입:       %s\n", pkg.Type)
		fmt.Printf("가격:       %s\n", price)
		fmt.Printf("다운로드:   %d\n", pkg.Downloads)
		if len(pkg.Tags) > 0 {
			fmt.Printf("태그:       ")
			for i, t := range pkg.Tags {
				if i > 0 {
					fmt.Print(", ")
				}
				fmt.Print(t)
			}
			fmt.Println()
		}
		if pkg.Description != "" {
			fmt.Printf("설명:       %s\n", pkg.Description)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(infoCmd)
}
```

- [ ] **Step 3: 빌드 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
go build ./...
./morphso search --help
./morphso info --help
```
Expected: 빌드 성공, help 텍스트 출력

- [ ] **Step 4: 커밋**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
git add cmd/search.go cmd/info.go
git commit -m "feat: add search and info commands"
git push origin main
```

---

## Task 7: Installer (install plan 실행)

**Files:**
- Create: `internal/installer/installer.go`
- Create: `internal/installer/installer_test.go`

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/installer/installer_test.go`:

```go
package installer_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tojiuni/morphso/internal/installer"
)

func TestBuildCommand_NativePip(t *testing.T) {
	cmd := installer.BuildCommand("pip", "native", "gopedia", "1.0.0")
	assert.Equal(t, []string{"pip", "install", "gopedia==1.0.0"}, cmd)
}

func TestBuildCommand_NativePipNoVersion(t *testing.T) {
	cmd := installer.BuildCommand("pip", "native", "gopedia", "")
	assert.Equal(t, []string{"pip", "install", "gopedia"}, cmd)
}

func TestBuildCommand_NativeNpm(t *testing.T) {
	cmd := installer.BuildCommand("npm", "native", "gopedia", "2.0.0")
	assert.Equal(t, []string{"npm", "install", "-g", "gopedia@2.0.0"}, cmd)
}

func TestBuildCommand_NativeMcp(t *testing.T) {
	// mcp 타입은 pip로 처리
	cmd := installer.BuildCommand("mcp", "native", "gopedia", "1.0.0")
	assert.Equal(t, []string{"pip", "install", "gopedia==1.0.0"}, cmd)
}

func TestBuildCommand_Docker(t *testing.T) {
	cmd := installer.BuildCommand("mcp", "docker", "gopedia", "1.0.0")
	assert.Equal(t, []string{
		"docker", "run", "-d", "--name", "gopedia",
		"artifacts.toji.homes/gopedia:1.0.0",
	}, cmd)
}

func TestBuildCommand_DockerLatest(t *testing.T) {
	cmd := installer.BuildCommand("mcp", "docker", "gopedia", "")
	assert.Equal(t, []string{
		"docker", "run", "-d", "--name", "gopedia",
		"artifacts.toji.homes/gopedia:latest",
	}, cmd)
}

func TestBuildCommand_Helm(t *testing.T) {
	cmd := installer.BuildCommand("helm", "helm", "gopedia", "1.0.0")
	assert.Equal(t, []string{
		"helm", "install", "gopedia", "morphso/gopedia", "--version", "1.0.0",
	}, cmd)
}

func TestBuildCommand_K8s(t *testing.T) {
	cmd := installer.BuildCommand("mcp", "k8s", "gopedia", "1.0.0")
	assert.Equal(t, []string{
		"helm", "install", "gopedia", "morphso/gopedia", "--version", "1.0.0",
	}, cmd)
}

func TestCheckPrerequisite(t *testing.T) {
	// "go"는 테스트 환경에 존재
	assert.True(t, installer.CheckPrerequisite("go"))
	assert.False(t, installer.CheckPrerequisite("__nonexistent_tool_xyz__"))
}
```

- [ ] **Step 2: 테스트 실패 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
go test ./internal/installer/... -v 2>&1 | head -10
```
Expected: compile error

- [ ] **Step 3: `internal/installer/installer.go` 구현**

```go
package installer

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

const artifactBase = "artifacts.toji.homes"
const helmRepo = "morphso/gopedia" // helm repo name

// BuildCommand returns the shell command slice for the given package type + strategy.
func BuildCommand(pkgType, strategy, slug, version string) []string {
	switch strategy {
	case "native":
		return buildNative(pkgType, slug, version)
	case "docker":
		image := artifactBase + "/" + slug
		if version != "" {
			image += ":" + version
		} else {
			image += ":latest"
		}
		return []string{"docker", "run", "-d", "--name", slug, image}
	case "k8s", "helm":
		args := []string{"helm", "install", slug, "morphso/" + slug}
		if version != "" {
			args = append(args, "--version", version)
		}
		return args
	default:
		return buildNative(pkgType, slug, version)
	}
}

func buildNative(pkgType, slug, version string) []string {
	switch pkgType {
	case "npm":
		pkg := slug
		if version != "" {
			pkg += "@" + version
		}
		return []string{"npm", "install", "-g", pkg}
	case "brew":
		return []string{"brew", "install", slug}
	case "pip", "mcp", "recipe", "binary", "helm":
		fallthrough
	default:
		pkg := slug
		if version != "" {
			pkg += "==" + version
		}
		return []string{"pip", "install", pkg}
	}
}

// CheckPrerequisite returns true if the tool is available in PATH.
func CheckPrerequisite(tool string) bool {
	_, err := exec.LookPath(tool)
	return err == nil
}

// Run executes the command, streaming stdout/stderr to the given writer.
func Run(command []string, out io.Writer) error {
	if len(command) == 0 {
		return fmt.Errorf("empty command")
	}
	if out == nil {
		out = os.Stdout
	}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("command failed: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: 테스트 통과 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
go test ./internal/installer/... -v -race
```
Expected: 9개 PASS

- [ ] **Step 5: 커밋**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
git add internal/installer/
git commit -m "feat: add installer (BuildCommand + Run)"
git push origin main
```

---

## Task 8: install 커맨드

**Files:**
- Create: `cmd/install.go`

- [ ] **Step 1: `cmd/install.go` 작성**

```go
package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
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
)

var installCmd = &cobra.Command{
	Use:   "install <package>",
	Short: "패키지 설치 (AI 추천 strategy 자동 선택)",
	Args:  cobra.ExactArgs(1),
	RunE:  runInstall,
}

func init() {
	installCmd.Flags().StringVar(&installStrategy, "strategy", "", "strategy 강제 지정 (native|docker|k8s|helm)")
	installCmd.Flags().BoolVar(&installYes, "yes", false, "비대화형 모드 (추천 strategy 자동 수락)")
	installCmd.Flags().BoolVar(&installNative, "native", false, "--strategy=native 단축키")
	installCmd.Flags().BoolVar(&installDocker, "docker", false, "--strategy=docker 단축키")
	installCmd.Flags().BoolVar(&installK8s, "k8s", false, "--strategy=k8s 단축키")
	installCmd.Flags().BoolVar(&installHelm, "helm", false, "--strategy=helm 단축키")
	rootCmd.AddCommand(installCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	slug := args[0]

	// bool 플래그 → strategy 문자열로 변환
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

	// 1. OS 스펙 수집 (캐시 우선)
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

	// 3. 설치 strategy 추천
	var strategy, reason string
	if cfg.Token != "" {
		result, err := client.Recommend(slug, s, preferred)
		if err == nil {
			strategy = result.Strategy
			reason = result.Reason
		}
	}
	// hub 추천 실패 또는 미로그인 → 로컬 rule-based 폴백
	if strategy == "" {
		strategy = hub.LocalRecommend(s, preferred)
		reason = "로컬 rule-based 추천 (hub 미연결 또는 미로그인)"
	}

	// 4. 추천 결과 출력 + 확인
	fmt.Printf("\n추천: --%s\n", strategy)
	fmt.Printf("이유: %s\n\n", reason)

	if !installYes {
		fmt.Printf("진행하시겠습니까? [Y/n/native/docker/k8s/helm] ")
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))
		switch input {
		case "", "y", "yes":
			// 추천 strategy 수락
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
	requiredTool := prerequisiteForStrategy(strategy, pkg.Type)
	if requiredTool != "" && !installer.CheckPrerequisite(requiredTool) {
		return fmt.Errorf("'%s'가 설치되어 있지 않습니다. 먼저 설치하세요: %s", requiredTool, requiredTool)
	}

	// 6. 설치 실행
	command := installer.BuildCommand(pkg.Type, strategy, slug, "")
	fmt.Printf("\n실행: %s\n\n", strings.Join(command, " "))
	if err := installer.Run(command, os.Stdout); err != nil {
		return fmt.Errorf("설치 실패: %w", err)
	}

	// 7. 설치 이력 기록 (로그인 상태)
	if cfg.Token != "" {
		_ = client.RecordInstall(slug, "", strategy)
	}

	fmt.Printf("\n✓ '%s' 설치 완료!\n", slug)
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

- [ ] **Step 2: 빌드 및 help 확인**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
go build ./...
./morphso install --help
```
Expected:
```
패키지 설치 (AI 추천 strategy 자동 선택)

Usage:
  morphso install <package> [flags]

Flags:
      --docker           --strategy=docker 단축키
      --helm             --strategy=helm 단축키
  -h, --help             help for install
      --k8s              --strategy=k8s 단축키
      --native           --strategy=native 단축키
      --strategy string  strategy 강제 지정 (native|docker|k8s|helm)
      --yes              비대화형 모드 (추천 strategy 자동 수락)
```

- [ ] **Step 3: 커밋**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
git add cmd/install.go
git commit -m "feat: add install command with AI recommendation and strategy execution"
git push origin main
```

---

## Task 9: list 커맨드 + stub 커맨드

**Files:**
- Create: `cmd/list.go`
- Create: `cmd/remove.go`
- Create: `cmd/publish.go`

- [ ] **Step 1: `cmd/list.go` 작성**

```go
package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tojiuni/morphso/internal/config"
	"github.com/tojiuni/morphso/internal/hub"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "설치된 패키지 목록 (hub 이력 기반)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.DefaultLoad()
		if err != nil {
			return err
		}
		if hubURL != "" {
			cfg.HubURL = hubURL
		}
		if cfg.Token == "" {
			return fmt.Errorf("로그인이 필요합니다. 'morphso login'을 실행하세요.")
		}
		client := hub.NewClient(cfg.HubURL, cfg.Token)
		records, err := client.GetInstalls()
		if errors.Is(err, hub.ErrUnauthorized) {
			return fmt.Errorf("인증 만료. 'morphso login'으로 재로그인하세요.")
		}
		if err != nil {
			return fmt.Errorf("이력 조회 실패: %w", err)
		}
		if len(records) == 0 {
			fmt.Println("설치 이력 없음.")
			return nil
		}
		fmt.Printf("%-20s %-8s %-10s %s\n", "PACKAGE", "STRATEGY", "VERSION", "INSTALLED AT")
		fmt.Println("--------------------------------------------------------------")
		for _, r := range records {
			version := r.Version
			if version == "" {
				version = "-"
			}
			fmt.Printf("%-20s %-8s %-10s %s\n",
				r.PackageSlug, r.Strategy, version,
				r.InstalledAt.Format("2006-01-02 15:04"))
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(listCmd)
}
```

- [ ] **Step 2: `cmd/remove.go` 작성 (stub)**

```go
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var removeCmd = &cobra.Command{
	Use:   "remove <package>",
	Short: "패키지 제거",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("'%s' 제거 기능은 아직 구현되지 않았습니다.\n", args[0])
		return nil
	},
}

func init() {
	rootCmd.AddCommand(removeCmd)
}
```

- [ ] **Step 3: `cmd/publish.go` 작성 (stub)**

```go
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var publishCmd = &cobra.Command{
	Use:   "publish",
	Short: "패키지 배포",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("패키지 배포 기능은 아직 구현되지 않았습니다.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(publishCmd)
}
```

- [ ] **Step 4: 전체 빌드 + 테스트**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
go build ./...
go test ./... -v -race
```
Expected: 빌드 성공, 전체 테스트 PASS

- [ ] **Step 5: 커맨드 목록 확인**

```bash
./morphso --help
```
Expected: install, search, info, list, remove, publish, login, logout, whoami 모두 표시

- [ ] **Step 6: 커밋**

```bash
cd /Users/dong-hoshin/Documents/dev/morphso
git add cmd/list.go cmd/remove.go cmd/publish.go
git commit -m "feat: add list command and stub remove/publish"
git push origin main
```

---

## 스펙 자체 검토

**스펙 커버리지:**
- ✅ `morphso install <package>` AI 추천 → Task 8
- ✅ `--native / --docker / --k8s / --helm / --yes / --strategy` 플래그 → Task 8
- ✅ OS 스펙 수집 & 24h 캐시 → Task 3
- ✅ hub `/recommend` 연동 + 로컬 폴백 → Task 4, 8
- ✅ 추천 결과 출력 + 확인 프롬프트 → Task 8
- ✅ install 실행 (native pip/npm/brew, docker, k8s/helm) → Task 7
- ✅ ZITADEL Device Flow 로그인 → Task 5
- ✅ `morphso login/logout/whoami` → Task 5
- ✅ `morphso search` → Task 6
- ✅ `morphso info` → Task 6
- ✅ `morphso list` (hub 이력) → Task 9
- ✅ `morphso remove` (stub) → Task 9
- ✅ `morphso publish` (stub) → Task 9
- ✅ 설치 이력 hub 기록 → Task 8
- ✅ 필요 도구 존재 확인 (docker/helm/npm) → Task 8
- ✅ `~/.morphso/config.yaml` (hub_url, token) → Task 2
- ✅ `~/.morphso/spec.yaml` 캐시 → Task 3

**다음 계획:**
- **Plan 4**: neunexus K8s 배포 — morphso ns, CNPG DB, Vault 시크릿, Traefik IngressRoute, Woodpecker CI
