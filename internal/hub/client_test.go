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
	_, err := client.RecordInstall("gopedia", "1.0.0", "native", "", "user")
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
