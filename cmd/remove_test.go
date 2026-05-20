package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tojiuni/morphso/internal/hub"
	"github.com/tojiuni/morphso/internal/mcpclient"
)

func TestUnregisterFromMCPClients_CallsOnlyDetected(t *testing.T) {
	pkg := &hub.Package{
		Type: "mcp", Slug: "gopedia-mcp",
		MCPMetadata: &hub.MCPMetadata{ServerName: "gopedia"},
	}
	a := &fakeClient{name: "a", detected: true}
	b := &fakeClient{name: "b", detected: false}
	out := &bytes.Buffer{}
	unregisterFromClients(pkg, []mcpclient.MCPClient{a, b}, out)
	assert.Equal(t, []string{"gopedia"}, a.unregistered)
	assert.Empty(t, b.unregistered)
}

func TestUnregisterFromMCPClients_SkipsNonMCP(t *testing.T) {
	pkg := &hub.Package{Type: "npm", Slug: "foo"}
	a := &fakeClient{name: "a", detected: true}
	unregisterFromClients(pkg, []mcpclient.MCPClient{a}, &bytes.Buffer{})
	assert.Empty(t, a.unregistered)
}

func TestUnregisterFromMCPClients_NilMetadataSkipped(t *testing.T) {
	pkg := &hub.Package{Type: "mcp", Slug: "x", MCPMetadata: nil}
	a := &fakeClient{name: "a", detected: true}
	unregisterFromClients(pkg, []mcpclient.MCPClient{a}, &bytes.Buffer{})
	assert.Empty(t, a.unregistered)
}
