package hub_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
