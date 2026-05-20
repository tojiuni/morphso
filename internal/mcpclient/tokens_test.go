package mcpclient

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandTokens_Image(t *testing.T) {
	ctx := TokenContext{Image: "repo/img", Version: "1.0.0", Slug: "gopedia-mcp"}
	got, err := ExpandTokens([]string{"run", "{{image}}"}, ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"run", "repo/img:1.0.0"}, got)
}

func TestExpandTokens_ImageWithoutVersion(t *testing.T) {
	ctx := TokenContext{Image: "repo/img"}
	got, err := ExpandTokens([]string{"{{image}}"}, ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"repo/img"}, got)
}

func TestExpandTokens_VersionAndSlug(t *testing.T) {
	ctx := TokenContext{Slug: "x", Version: "2"}
	got, err := ExpandTokens([]string{"--name", "{{slug}}-{{version}}"}, ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"--name", "x-2"}, got)
}

func TestExpandTokens_NoTokens(t *testing.T) {
	got, err := ExpandTokens([]string{"plain", "args"}, TokenContext{})
	require.NoError(t, err)
	assert.Equal(t, []string{"plain", "args"}, got)
}

func TestExpandTokens_UnknownTokenError(t *testing.T) {
	_, err := ExpandTokens([]string{"{{unknown}}"}, TokenContext{})
	assert.ErrorContains(t, err, "unknown token")
}

func TestExpandTokens_EnvTokenRejected(t *testing.T) {
	// {{env:VAR}} is not supported at expansion time — hub validator forbids
	// it from appearing in args. We treat it as an unknown token to be safe.
	_, err := ExpandTokens([]string{"{{env:FOO}}"}, TokenContext{})
	assert.ErrorContains(t, err, "unknown token")
}

func TestExpandTokens_EmptyArgs(t *testing.T) {
	got, err := ExpandTokens([]string{}, TokenContext{})
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestExpandTokens_NilArgs(t *testing.T) {
	got, err := ExpandTokens(nil, TokenContext{})
	require.NoError(t, err)
	assert.Empty(t, got)
}
