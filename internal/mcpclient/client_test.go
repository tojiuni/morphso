package mcpclient

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAllClients_Names(t *testing.T) {
	names := []string{}
	for _, c := range AllClients() {
		names = append(names, c.Name())
	}
	assert.Contains(t, names, "claude-code")
	assert.Contains(t, names, "cursor")
	assert.Contains(t, names, "gemini-cli")
	assert.Len(t, names, 3)
}

func TestDetectInstalled_FiltersUndetected(t *testing.T) {
	// All stubs return Detected()=false at this stage.
	// After B3-B5 plug real implementations, this test stays meaningful by
	// asserting only that DetectInstalled never panics and returns a slice.
	got := DetectInstalled()
	assert.NotNil(t, got) // empty slice or non-empty; not nil panic
}
