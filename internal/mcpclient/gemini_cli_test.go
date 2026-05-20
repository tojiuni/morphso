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

func TestGemini_Detected_Absent(t *testing.T) {
	geminiTempHome(t)
	// Detected falls back to checking `gemini` binary in PATH if directory absent.
	// In tests we can't guarantee binary state — just confirm the call doesn't panic.
	_ = newGeminiCLI().Detected()
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
	env, _ := entry["env"].(map[string]any)
	assert.Equal(t, "1", env["X"])
}

func TestGemini_Register_PreservesOtherTopLevelKeys(t *testing.T) {
	home := geminiTempHome(t)
	dir := filepath.Join(home, ".gemini")
	require.NoError(t, os.MkdirAll(dir, 0700))
	original := []byte(`{"ide":{"hasSeenNudge":true},"security":{"auth":"x"},"GEMINI_SYSTEM_MD":"prompt..."}`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "settings.json"), original, 0600))

	require.NoError(t, newGeminiCLI().Register("gopedia", MCPServerEntry{Command: "x"}))

	var got map[string]any
	data, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Contains(t, got, "ide")
	assert.Contains(t, got, "security")
	assert.Contains(t, got, "GEMINI_SYSTEM_MD")
	assert.Contains(t, got, "mcp")
}

func TestGemini_Register_PreservesExistingMCPSubkeys(t *testing.T) {
	// If user already has mcp.somethingElse, preserve it.
	home := geminiTempHome(t)
	dir := filepath.Join(home, ".gemini")
	require.NoError(t, os.MkdirAll(dir, 0700))
	original := []byte(`{"mcp":{"otherKey":"keep","servers":{"existing":{"command":"old"}}}}`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "settings.json"), original, 0600))

	require.NoError(t, newGeminiCLI().Register("new", MCPServerEntry{Command: "x"}))

	var got map[string]any
	data, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	require.NoError(t, json.Unmarshal(data, &got))
	mcp, _ := got["mcp"].(map[string]any)
	assert.Equal(t, "keep", mcp["otherKey"])
	servers, _ := mcp["servers"].(map[string]any)
	assert.Contains(t, servers, "existing")
	assert.Contains(t, servers, "new")
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

func TestGemini_Unregister_Removes(t *testing.T) {
	home := geminiTempHome(t)
	dir := filepath.Join(home, ".gemini")
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, newGeminiCLI().Register("foo", MCPServerEntry{Command: "x"}))
	require.NoError(t, newGeminiCLI().Unregister("foo"))

	data, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var got map[string]any
	_ = json.Unmarshal(data, &got)
	mcp, _ := got["mcp"].(map[string]any)
	servers, _ := mcp["servers"].(map[string]any)
	assert.NotContains(t, servers, "foo")
}

func TestGemini_ExistingEntry(t *testing.T) {
	home := geminiTempHome(t)
	dir := filepath.Join(home, ".gemini")
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, newGeminiCLI().Register("g", MCPServerEntry{Command: "x", Env: map[string]string{"A": "1"}}))

	got, err := newGeminiCLI().ExistingEntry("g")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "x", got.Command)
	assert.Equal(t, "1", got.Env["A"])

	missing, err := newGeminiCLI().ExistingEntry("nope")
	require.NoError(t, err)
	assert.Nil(t, missing)
}
