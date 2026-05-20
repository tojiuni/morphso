package hub_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestPackageJSON_WithMCPMetadata(t *testing.T) {
	jsonStr := `{
	  "slug":"gopedia-mcp","type":"mcp",
	  "mcp_metadata":{
	    "server_name":"gopedia","transport":"stdio",
	    "native":{"command":"gopedia-mcp-server"},
	    "env_schema":[{"name":"GOPEDIA_HOST_DOMAIN","required":true,"default":"127.0.0.1:18787"}]
	  }
	}`
	var p hub.Package
	require.NoError(t, json.Unmarshal([]byte(jsonStr), &p))
	require.NotNil(t, p.MCPMetadata)
	assert.Equal(t, "gopedia", p.MCPMetadata.ServerName)
	assert.Equal(t, "stdio", p.MCPMetadata.Transport)
	assert.Equal(t, "gopedia-mcp-server", p.MCPMetadata.Native.Command)
	assert.Equal(t, "GOPEDIA_HOST_DOMAIN", p.MCPMetadata.EnvSchema[0].Name)
}

func TestPackageJSON_WithoutMCPMetadata(t *testing.T) {
	jsonStr := `{"slug":"foo","type":"npm"}`
	var p hub.Package
	require.NoError(t, json.Unmarshal([]byte(jsonStr), &p))
	assert.Nil(t, p.MCPMetadata)
}
