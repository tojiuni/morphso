package spec_test

import (
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
		CollectedAt: time.Now().Add(-25 * time.Hour),
		OS:          "linux",
	}
	require.NoError(t, spec.SaveCache(dir, s))

	loaded, err := spec.LoadCache(dir)
	require.NoError(t, err)
	assert.Nil(t, loaded)
}

func TestCache_MissingReturnsNil(t *testing.T) {
	dir := t.TempDir()
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
