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
