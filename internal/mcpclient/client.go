// Package mcpclient defines an interface for registering MCP servers with
// installed MCP clients (Claude Code, Cursor, Gemini CLI). Each client
// implementation owns its own config JSON path and write strategy —
// the interface assumes nothing about config layout.
package mcpclient

// MCPClient encapsulates per-client config layout. Implementations:
//   - Claude Code  → subprocess (claude mcp add-json) with direct-edit fallback
//   - Cursor       → ~/.cursor/mcp.json (flat mcpServers)
//   - Gemini CLI   → ~/.gemini/settings.json (nested mcp.servers)
type MCPClient interface {
	Name() string
	Detected() bool
	ExistingEntry(serverName string) (*MCPServerEntry, error)
	Register(serverName string, entry MCPServerEntry) error
	Unregister(serverName string) error
}

// MCPServerEntry is the per-server payload as written to each client's
// config file. JSON tags match the common shape used by all three clients
// (Claude Code, Cursor, Gemini CLI all accept command/args/env).
type MCPServerEntry struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// AllClients returns the canonical list of MCP clients morphso knows how to
// register against. Order is stable.
func AllClients() []MCPClient {
	return []MCPClient{
		newClaudeCode(),
		newCursor(),
		newGeminiCLI(),
	}
}

// DetectInstalled returns only the clients reporting Detected()=true.
func DetectInstalled() []MCPClient {
	out := []MCPClient{}
	for _, c := range AllClients() {
		if c.Detected() {
			out = append(out, c)
		}
	}
	return out
}

// Stubs replaced in B3 (cursor), B4 (gemini), B5 (claude-code).
// Defining them up-front so this scaffold compiles standalone.
func newClaudeCode() MCPClient { return &stubClient{name: "claude-code"} }
func newCursor() MCPClient     { return newCursorReal() }
func newGeminiCLI() MCPClient  { return &stubClient{name: "gemini-cli"} }

type stubClient struct{ name string }

func (s *stubClient) Name() string                                  { return s.name }
func (s *stubClient) Detected() bool                                { return false }
func (s *stubClient) ExistingEntry(string) (*MCPServerEntry, error) { return nil, nil }
func (s *stubClient) Register(string, MCPServerEntry) error         { return nil }
func (s *stubClient) Unregister(string) error                       { return nil }
