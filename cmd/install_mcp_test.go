package cmd

import (
	"bufio"
	"bytes"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tojiuni/morphso/internal/hub"
	"github.com/tojiuni/morphso/internal/mcpclient"
)

// fakeClient is a test double for mcpclient.MCPClient — captures Register/Unregister
// calls and lets us seed ExistingEntry. Reused by B10 tests.
type fakeClient struct {
	name         string
	detected     bool
	existing     *mcpclient.MCPServerEntry
	registered   map[string]mcpclient.MCPServerEntry
	unregistered []string
}

func (f *fakeClient) Name() string   { return f.name }
func (f *fakeClient) Detected() bool { return f.detected }
func (f *fakeClient) ExistingEntry(string) (*mcpclient.MCPServerEntry, error) {
	return f.existing, nil
}
func (f *fakeClient) Register(name string, e mcpclient.MCPServerEntry) error {
	if f.registered == nil {
		f.registered = map[string]mcpclient.MCPServerEntry{}
	}
	f.registered[name] = e
	return nil
}
func (f *fakeClient) Unregister(name string) error {
	f.unregistered = append(f.unregistered, name)
	return nil
}

func TestRunMCPRegistration_NativeBasic(t *testing.T) {
	meta := &hub.MCPMetadata{
		ServerName: "gopedia", Transport: "stdio",
		Native: &hub.MCPCommandSpec{Command: "gopedia-mcp-server"},
		EnvSchema: []hub.MCPEnvSpec{
			{Name: "GOPEDIA_HOST_DOMAIN", Required: true, Default: "127.0.0.1:18787"},
		},
	}
	pkg := &hub.Package{Slug: "gopedia-mcp", Type: "mcp", MCPMetadata: meta}
	client := &fakeClient{name: "fake", detected: true}

	reader := bufio.NewReader(strings.NewReader("\n")) // accept default
	out := &bytes.Buffer{}
	err := runMCPRegistrationWithClients(pkg, "native", "1.0.0", reader, out,
		[]mcpclient.MCPClient{client}, false, false)
	require.NoError(t, err)

	got, ok := client.registered["gopedia"]
	require.True(t, ok)
	assert.Equal(t, "gopedia-mcp-server", got.Command)
	assert.Equal(t, "127.0.0.1:18787", got.Env["GOPEDIA_HOST_DOMAIN"])
}

func TestRunMCPRegistration_TransportUnsupported(t *testing.T) {
	pkg := &hub.Package{Slug: "x", Type: "mcp", MCPMetadata: &hub.MCPMetadata{
		ServerName: "x", Transport: "http",
		Native: &hub.MCPCommandSpec{Command: "x"},
	}}
	err := runMCPRegistrationWithClients(pkg, "native", "1", bufio.NewReader(strings.NewReader("")), &bytes.Buffer{}, nil, true, false)
	assert.ErrorContains(t, err, "transport")
}

func TestRunMCPRegistration_WindowsSkip(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only path")
	}
	pkg := &hub.Package{Slug: "x", Type: "mcp", MCPMetadata: &hub.MCPMetadata{
		ServerName: "x", Native: &hub.MCPCommandSpec{Command: "x"},
	}}
	out := &bytes.Buffer{}
	err := runMCPRegistrationWithClients(pkg, "native", "1", bufio.NewReader(strings.NewReader("")), out, nil, true, false)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "windows registration not implemented")
}

func TestRunMCPRegistration_ReusesExistingEnvAsDefault(t *testing.T) {
	meta := &hub.MCPMetadata{
		ServerName: "gopedia", Transport: "stdio",
		Native:    &hub.MCPCommandSpec{Command: "x"},
		EnvSchema: []hub.MCPEnvSpec{{Name: "FOO", Required: true}},
	}
	pkg := &hub.Package{Slug: "x", Type: "mcp", MCPMetadata: meta}
	client := &fakeClient{name: "f", detected: true,
		existing: &mcpclient.MCPServerEntry{Command: "x", Env: map[string]string{"FOO": "old-value"}},
	}
	reader := bufio.NewReader(strings.NewReader("\n")) // accept default = existing
	err := runMCPRegistrationWithClients(pkg, "native", "1", reader, &bytes.Buffer{}, []mcpclient.MCPClient{client}, false, false)
	require.NoError(t, err)
	assert.Equal(t, "old-value", client.registered["gopedia"].Env["FOO"])
}

func TestRunMCPRegistration_ReconfigureIgnoresExisting(t *testing.T) {
	meta := &hub.MCPMetadata{
		ServerName: "gopedia", Transport: "stdio",
		Native:    &hub.MCPCommandSpec{Command: "x"},
		EnvSchema: []hub.MCPEnvSpec{{Name: "FOO", Required: true, Default: "default-val"}},
	}
	pkg := &hub.Package{Slug: "x", Type: "mcp", MCPMetadata: meta}
	client := &fakeClient{detected: true,
		existing: &mcpclient.MCPServerEntry{Env: map[string]string{"FOO": "old-value"}},
	}
	// reconfigure=true: should use schema Default, not existing.
	reader := bufio.NewReader(strings.NewReader("\n"))
	err := runMCPRegistrationWithClients(pkg, "native", "1", reader, &bytes.Buffer{}, []mcpclient.MCPClient{client}, false, true)
	require.NoError(t, err)
	assert.Equal(t, "default-val", client.registered["gopedia"].Env["FOO"])
}

func TestRunMCPRegistration_YesUsesEnvFallback(t *testing.T) {
	t.Setenv("FOO", "from-env")
	meta := &hub.MCPMetadata{
		ServerName: "x", Transport: "stdio",
		Native:    &hub.MCPCommandSpec{Command: "x"},
		EnvSchema: []hub.MCPEnvSpec{{Name: "FOO", Required: true}},
	}
	pkg := &hub.Package{Slug: "x", Type: "mcp", MCPMetadata: meta}
	client := &fakeClient{name: "f", detected: true}
	err := runMCPRegistrationWithClients(pkg, "native", "1", bufio.NewReader(strings.NewReader("")), &bytes.Buffer{}, []mcpclient.MCPClient{client}, true, false)
	require.NoError(t, err)
	assert.Equal(t, "from-env", client.registered["x"].Env["FOO"])
}

func TestRunMCPRegistration_MissingRequiredYesErrors(t *testing.T) {
	meta := &hub.MCPMetadata{
		ServerName: "x", Native: &hub.MCPCommandSpec{Command: "x"},
		EnvSchema: []hub.MCPEnvSpec{{Name: "FOO", Required: true}},
	}
	pkg := &hub.Package{Slug: "x", Type: "mcp", MCPMetadata: meta}
	err := runMCPRegistrationWithClients(pkg, "native", "1", bufio.NewReader(strings.NewReader("")), &bytes.Buffer{}, []mcpclient.MCPClient{&fakeClient{detected: true}}, true, false)
	assert.ErrorContains(t, err, "FOO")
}

func TestRunMCPRegistration_DockerImageTokenExpanded(t *testing.T) {
	meta := &hub.MCPMetadata{
		ServerName: "x", Transport: "stdio",
		Docker: &hub.MCPCommandSpec{Image: "repo/img", Args: []string{"run", "-i", "--rm", "{{image}}"}},
	}
	pkg := &hub.Package{Slug: "x", Type: "mcp", MCPMetadata: meta}
	client := &fakeClient{detected: true}
	err := runMCPRegistrationWithClients(pkg, "docker", "1.0", bufio.NewReader(strings.NewReader("")), &bytes.Buffer{}, []mcpclient.MCPClient{client}, true, false)
	require.NoError(t, err)
	got := client.registered["x"]
	assert.Equal(t, "docker", got.Command)
	assert.Equal(t, []string{"run", "-i", "--rm", "repo/img:1.0"}, got.Args)
}

func TestRunMCPRegistration_NoOutputContainsEnvValue(t *testing.T) {
	t.Setenv("SECRET_KEY", "supersecret123")
	meta := &hub.MCPMetadata{
		ServerName: "x", Transport: "stdio",
		Native:    &hub.MCPCommandSpec{Command: "x"},
		EnvSchema: []hub.MCPEnvSpec{{Name: "SECRET_KEY", Required: true, Secret: true}},
	}
	pkg := &hub.Package{Slug: "x", Type: "mcp", MCPMetadata: meta}
	out := &bytes.Buffer{}
	err := runMCPRegistrationWithClients(pkg, "native", "1", bufio.NewReader(strings.NewReader("")), out, []mcpclient.MCPClient{&fakeClient{detected: true}}, true, false)
	require.NoError(t, err)
	assert.NotContains(t, out.String(), "supersecret123")
}

func TestRunMCPRegistration_UnregisteredEnvToken_NotInExpansion(t *testing.T) {
	// ExpandTokens does not support {{env:FOO}} — should error.
	meta := &hub.MCPMetadata{
		ServerName: "x", Native: &hub.MCPCommandSpec{Command: "x", Args: []string{"{{env:FOO}}"}},
		EnvSchema: []hub.MCPEnvSpec{{Name: "FOO", Required: true, Default: "v"}},
	}
	pkg := &hub.Package{Slug: "x", Type: "mcp", MCPMetadata: meta}
	err := runMCPRegistrationWithClients(pkg, "native", "1", bufio.NewReader(strings.NewReader("")), &bytes.Buffer{}, []mcpclient.MCPClient{&fakeClient{detected: true}}, true, false)
	assert.Error(t, err)
}
