package cmd

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tojiuni/morphso/internal/hub"
	"github.com/tojiuni/morphso/internal/spec"
)

// e2eScript computes the SHA256 so the integrity check in runScriptFlow passes.
func e2eScript(content string) hub.InstallScript {
	h := sha256.Sum256([]byte(content))
	return hub.InstallScript{
		Script:  content,
		SHA256:  fmt.Sprintf("sha256:%x", h),
		Version: "latest",
	}
}

const (
	e2eOkScript   = "#!/bin/sh\ntrue\n"
	e2eFailScript = "#!/bin/sh\nexit 1\n"
)

// e2eHub is an httptest stub for the morphso-hub endpoints exercised by runInstall.
type e2eHub struct {
	*httptest.Server
	pkgs     map[string]hub.Package
	scripts  map[string]hub.InstallScript // missing slug → 404
	deps     map[string]hub.DependencyResponse
	recorded []string // package_slug from each POST /installs
}

func newE2EHub(t *testing.T) *e2eHub {
	h := &e2eHub{
		pkgs:    make(map[string]hub.Package),
		scripts: make(map[string]hub.InstallScript),
		deps:    make(map[string]hub.DependencyResponse),
	}

	mux := http.NewServeMux()

	// GET /packages/<slug>[/install-script|/dependencies]
	mux.HandleFunc("/packages/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/packages/")
		slug, sub, _ := strings.Cut(path, "/")
		w.Header().Set("Content-Type", "application/json")
		switch sub {
		case "install-script":
			sc, ok := h.scripts[slug]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"error":"not found"}`)
				return
			}
			json.NewEncoder(w).Encode(sc)
		case "dependencies":
			json.NewEncoder(w).Encode(h.deps[slug]) // zero value → empty deps
		default:
			pkg, ok := h.pkgs[slug]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			json.NewEncoder(w).Encode(pkg)
		}
	})

	// POST /installs
	mux.HandleFunc("/installs", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			PackageSlug string `json:"package_slug"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		h.recorded = append(h.recorded, body.PackageSlug)
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"id":"test-id"}`)
	})

	// GET /users/me/installs (dep planning needs install history)
	mux.HandleFunc("/users/me/installs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[]`)
	})

	h.Server = httptest.NewServer(mux)
	t.Cleanup(h.Server.Close)
	return h
}

// seedHome creates a temp HOME directory with config.yaml (hub URL + token) and a
// pre-cached spec so runInstall doesn't attempt live OS collection.
func seedHome(t *testing.T, hubURL string) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	morphsoDir := filepath.Join(home, ".morphso")
	require.NoError(t, os.MkdirAll(morphsoDir, 0755))

	cfgContent := fmt.Sprintf("hub_url: %s\ntoken: test-token\n", hubURL)
	require.NoError(t, os.WriteFile(filepath.Join(morphsoDir, "config.yaml"), []byte(cfgContent), 0600))

	s := &spec.Spec{
		CollectedAt:    time.Now(),
		OS:             "linux",
		Arch:           "amd64",
		MemoryTotalGB:  16,
		MemoryFreeGB:   8,
		DiskTotalGB:    100,
		DiskFreeGB:     50,
		InstalledTools: map[string]string{},
	}
	require.NoError(t, spec.SaveCache(morphsoDir, s))
}

// saveInstallFlags saves package-level install flag state and restores on cleanup.
func saveInstallFlags(t *testing.T) {
	yes, noDeps, native, docker, k8s, helm := installYes, installNoDeps, installNative, installDocker, installK8s, installHelm
	strategy, cfg, tmpl, reconf := installStrategy, installConfig, installTemplate, installReconfigure
	t.Cleanup(func() {
		installYes, installNoDeps = yes, noDeps
		installNative, installDocker, installK8s, installHelm = native, docker, k8s, helm
		installStrategy, installConfig, installTemplate, installReconfigure = strategy, cfg, tmpl, reconf
	})
}

// redirectStdin replaces os.Stdin with a pipe whose write end contains input.
func redirectStdin(t *testing.T, input string) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old; r.Close() })
	go func() { io.WriteString(w, input); w.Close() }()
}

// ── tests ───────────────────────────────────────────────────────────────────

// TestInstallE2E_HappyPath verifies the minimal success path: package with no
// dependencies installs and is recorded.
func TestInstallE2E_HappyPath(t *testing.T) {
	h := newE2EHub(t)
	h.pkgs["gopedia"] = hub.Package{Slug: "gopedia", Name: "Gopedia", Type: "binary"}
	h.scripts["gopedia"] = e2eScript(e2eOkScript)

	seedHome(t, h.URL)
	saveInstallFlags(t)
	installYes = true
	installNoDeps = true

	require.NoError(t, runInstall(installCmd, []string{"gopedia"}))
	assert.Contains(t, h.recorded, "gopedia")
}

// TestInstallE2E_RequiredDep_InstallsBeforeMain verifies that a required dependency
// is installed and recorded before the main package.
func TestInstallE2E_RequiredDep_InstallsBeforeMain(t *testing.T) {
	h := newE2EHub(t)
	h.pkgs["gopedia"] = hub.Package{Slug: "gopedia", Name: "Gopedia", Type: "binary"}
	h.pkgs["ollama"] = hub.Package{Slug: "ollama", Name: "Ollama", Type: "binary"}
	h.scripts["gopedia"] = e2eScript(e2eOkScript)
	h.scripts["ollama"] = e2eScript(e2eOkScript)
	h.deps["gopedia"] = hub.DependencyResponse{
		Dependencies: []hub.DependencyInfo{
			{Package: hub.Package{Slug: "ollama", Name: "Ollama"}, MinVersion: "latest"},
		},
	}

	seedHome(t, h.URL)
	saveInstallFlags(t)
	installYes = true

	require.NoError(t, runInstall(installCmd, []string{"gopedia"}))
	require.Len(t, h.recorded, 2)
	assert.Equal(t, "ollama", h.recorded[0], "dep must be recorded before main")
	assert.Equal(t, "gopedia", h.recorded[1])
}

// TestInstallE2E_RequiredDep_FailureHaltsInstall verifies fix from PR #6:
// a failing required dependency stops the install with an error and the main
// package is never installed.
func TestInstallE2E_RequiredDep_FailureHaltsInstall(t *testing.T) {
	h := newE2EHub(t)
	h.pkgs["gopedia"] = hub.Package{Slug: "gopedia", Name: "Gopedia", Type: "binary"}
	h.pkgs["ollama"] = hub.Package{Slug: "ollama", Name: "Ollama", Type: "binary"}
	h.scripts["gopedia"] = e2eScript(e2eOkScript)
	h.scripts["ollama"] = e2eScript(e2eFailScript) // exits 1
	h.deps["gopedia"] = hub.DependencyResponse{
		Dependencies: []hub.DependencyInfo{
			{Package: hub.Package{Slug: "ollama", Name: "Ollama"}, MinVersion: "latest"},
		},
	}

	seedHome(t, h.URL)
	saveInstallFlags(t)
	installYes = true

	err := runInstall(installCmd, []string{"gopedia"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ollama")
	assert.NotContains(t, h.recorded, "gopedia", "main must not be recorded after dep failure")
}

// TestInstallE2E_OptionalDep_UserInstalls verifies that choosing [1] (install)
// for an optional dep installs it and records both packages.
func TestInstallE2E_OptionalDep_UserInstalls(t *testing.T) {
	h := newE2EHub(t)
	h.pkgs["gopedia"] = hub.Package{Slug: "gopedia", Name: "Gopedia", Type: "binary"}
	h.pkgs["ollama"] = hub.Package{Slug: "ollama", Name: "Ollama", Type: "binary"}
	h.scripts["gopedia"] = e2eScript(e2eOkScript)
	h.scripts["ollama"] = e2eScript(e2eOkScript)
	h.deps["gopedia"] = hub.DependencyResponse{
		Dependencies: []hub.DependencyInfo{
			{Package: hub.Package{Slug: "ollama", Name: "Ollama"}, MinVersion: "latest", Optional: true},
		},
	}

	seedHome(t, h.URL)
	saveInstallFlags(t)
	installYes = false // interactive: 사용자가 [1] 직접 선택
	redirectStdin(t, "y\n1\n") // strategy 확인 y → 옵션 [1] install

	require.NoError(t, runInstall(installCmd, []string{"gopedia"}))
	assert.Contains(t, h.recorded, "ollama")
	assert.Contains(t, h.recorded, "gopedia")
}

// TestInstallE2E_OptionalDep_UserProvidesURL verifies that choosing [2] (URL)
// skips the dep install but proceeds with the main package.
func TestInstallE2E_OptionalDep_UserProvidesURL(t *testing.T) {
	h := newE2EHub(t)
	h.pkgs["gopedia"] = hub.Package{Slug: "gopedia", Name: "Gopedia", Type: "binary"}
	h.scripts["gopedia"] = e2eScript(e2eOkScript)
	h.deps["gopedia"] = hub.DependencyResponse{
		Dependencies: []hub.DependencyInfo{
			{Package: hub.Package{Slug: "ollama", Name: "Ollama"}, MinVersion: "latest", Optional: true},
		},
	}

	seedHome(t, h.URL)
	saveInstallFlags(t)
	installYes = false // interactive: 사용자가 [2] URL 직접 입력
	redirectStdin(t, "y\n2\nhttp://custom:11434\n") // strategy 확인 y → 옵션 [2] URL

	require.NoError(t, runInstall(installCmd, []string{"gopedia"}))
	assert.Contains(t, h.recorded, "gopedia")
	assert.NotContains(t, h.recorded, "ollama", "ollama must not be recorded when user chose URL instead")
}

// TestInstallE2E_OptionalDep_UserSkips verifies that choosing [4] (skip) for an
// optional dep still allows the main package to install.
func TestInstallE2E_OptionalDep_UserSkips(t *testing.T) {
	h := newE2EHub(t)
	h.pkgs["gopedia"] = hub.Package{Slug: "gopedia", Name: "Gopedia", Type: "binary"}
	h.scripts["gopedia"] = e2eScript(e2eOkScript)
	h.deps["gopedia"] = hub.DependencyResponse{
		Dependencies: []hub.DependencyInfo{
			{Package: hub.Package{Slug: "ollama", Name: "Ollama"}, MinVersion: "latest", Optional: true},
		},
	}

	seedHome(t, h.URL)
	saveInstallFlags(t)
	installYes = false // interactive: 사용자가 [4] 건너뜀 직접 선택
	redirectStdin(t, "y\n4\n") // strategy 확인 y → 옵션 [4] skip

	require.NoError(t, runInstall(installCmd, []string{"gopedia"}))
	assert.Contains(t, h.recorded, "gopedia")
	assert.NotContains(t, h.recorded, "ollama")
}

// TestInstallE2E_OptionalDep_YesAutoInstalls verifies that with --yes (installYes),
// an optional dep is auto-selected as [1] (install) WITHOUT reading stdin — the
// non-interactive flow must not block on the "선택 [1-4]:" prompt.
func TestInstallE2E_OptionalDep_YesAutoInstalls(t *testing.T) {
	h := newE2EHub(t)
	h.pkgs["gopedia"] = hub.Package{Slug: "gopedia", Name: "Gopedia", Type: "binary"}
	h.pkgs["ollama"] = hub.Package{Slug: "ollama", Name: "Ollama", Type: "binary"}
	h.scripts["gopedia"] = e2eScript(e2eOkScript)
	h.scripts["ollama"] = e2eScript(e2eOkScript)
	h.deps["gopedia"] = hub.DependencyResponse{
		Dependencies: []hub.DependencyInfo{
			{Package: hub.Package{Slug: "ollama", Name: "Ollama"}, MinVersion: "latest", Optional: true},
		},
	}

	seedHome(t, h.URL)
	saveInstallFlags(t)
	installYes = true
	// no redirectStdin: --yes must NOT block on stdin for the optional choice

	require.NoError(t, runInstall(installCmd, []string{"gopedia"}))
	assert.Contains(t, h.recorded, "ollama", "optional dep auto-installed under --yes")
	assert.Contains(t, h.recorded, "gopedia")
}
