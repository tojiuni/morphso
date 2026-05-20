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

func TestCursor_Unregister_Idempotent_NoFile(t *testing.T) {
	cursorTempHome(t)
	assert.NoError(t, newCursor().Unregister("never-existed"))
}

func TestCursor_Unregister_RemovesExisting(t *testing.T) {
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

func TestCursor_FilePermissions(t *testing.T) {
	home := cursorTempHome(t)
	dir := filepath.Join(home, ".cursor")
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, newCursor().Register("x", MCPServerEntry{Command: "x"}))

	info, err := os.Stat(filepath.Join(dir, "mcp.json"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}
