package mcpclient

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// claudeTempHome sets HOME to a temp dir and disables the claude CLI
// subprocess path so tests exercise the direct-edit fallback deterministically.
// The subprocess path is verified separately by integration tests.
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
	env, _ := entry["env"].(map[string]any)
	assert.Equal(t, "127.0.0.1:18787", env["GOPEDIA_HOST_DOMAIN"])
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
	assert.Contains(t, got, "mcpServers")
}

func TestClaude_RefusesCorruptJSON(t *testing.T) {
	home := claudeTempHome(t)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".claude.json"), []byte("broken"), 0600))
	err := newClaudeCode().Register("gopedia", MCPServerEntry{Command: "x"})
	assert.ErrorContains(t, err, "corrupt")
}

func TestClaude_Unregister_Idempotent(t *testing.T) {
	claudeTempHome(t)
	assert.NoError(t, newClaudeCode().Unregister("never-existed"))
}

func TestClaude_Unregister_Removes(t *testing.T) {
	home := claudeTempHome(t)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{}`), 0600))
	require.NoError(t, newClaudeCode().Register("foo", MCPServerEntry{Command: "x"}))
	require.NoError(t, newClaudeCode().Unregister("foo"))

	data, _ := os.ReadFile(filepath.Join(home, ".claude.json"))
	var got map[string]any
	_ = json.Unmarshal(data, &got)
	servers, _ := got["mcpServers"].(map[string]any)
	assert.NotContains(t, servers, "foo")
}

func TestClaude_ExistingEntry(t *testing.T) {
	home := claudeTempHome(t)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{}`), 0600))
	require.NoError(t, newClaudeCode().Register("g", MCPServerEntry{
		Command: "x", Env: map[string]string{"A": "1"},
	}))
	got, err := newClaudeCode().ExistingEntry("g")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "x", got.Command)
	assert.Equal(t, "1", got.Env["A"])
}
