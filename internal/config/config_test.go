package config_test

import (
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
