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
