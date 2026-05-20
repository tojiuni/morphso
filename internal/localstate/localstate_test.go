package localstate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(v)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMCPRegistrations_MergesClientsAndExtractsImage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dockerEntry := map[string]any{
		"command": "docker",
		"args":    []string{"run", "-i", "--rm", "-e", "GOPEDIA_HOST_DOMAIN", "artifacts.toji.homes/neunexus/gopedia-mcp:latest"},
	}
	// claude-code: gopedia registered
	writeJSON(t, filepath.Join(home, ".claude.json"), map[string]any{
		"mcpServers": map[string]any{"gopedia": dockerEntry},
	})
	// cursor: gopedia registered too
	writeJSON(t, filepath.Join(home, ".cursor", "mcp.json"), map[string]any{
		"mcpServers": map[string]any{"gopedia": dockerEntry},
	})
	// gemini: a different server (nested layout)
	writeJSON(t, filepath.Join(home, ".gemini", "settings.json"), map[string]any{
		"mcp": map[string]any{"servers": map[string]any{
			"other": map[string]any{"command": "npx", "args": []string{"-y", "other-mcp"}},
		}},
	})

	regs, err := MCPRegistrations()
	if err != nil {
		t.Fatalf("MCPRegistrations: %v", err)
	}

	byServer := map[string]Registration{}
	for _, r := range regs {
		byServer[r.Server] = r
	}

	g, ok := byServer["gopedia"]
	if !ok {
		t.Fatal("expected 'gopedia' registration")
	}
	if g.Image != "artifacts.toji.homes/neunexus/gopedia-mcp:latest" {
		t.Errorf("image = %q, want gopedia-mcp image", g.Image)
	}
	if len(g.Clients) != 2 {
		t.Errorf("gopedia clients = %v, want claude-code + cursor", g.Clients)
	}

	o, ok := byServer["other"]
	if !ok {
		t.Fatal("expected 'other' registration")
	}
	if o.Image != "" {
		t.Errorf("non-docker entry should have empty image, got %q", o.Image)
	}

	// A non-docker command (e.g. a postgres MCP server) whose args contain a
	// URL-like string must NOT be misread as a docker image.
	writeJSON(t, filepath.Join(home, ".cursor", "mcp.json"), map[string]any{
		"mcpServers": map[string]any{
			"gopedia": dockerEntry,
			"postgres": map[string]any{
				"command": "postgres-mcp",
				"args":    []string{"postgresql://user:pass@localhost:5432/db"},
			},
		},
	})
	regs2, _ := MCPRegistrations()
	for _, r := range regs2 {
		if r.Server == "postgres" && r.Image != "" {
			t.Errorf("postgres (non-docker) image = %q, want empty", r.Image)
		}
	}
	if len(o.Clients) != 1 || o.Clients[0] != "gemini-cli" {
		t.Errorf("other clients = %v, want [gemini-cli]", o.Clients)
	}
}

func TestMCPRegistrations_NoConfigs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	regs, err := MCPRegistrations()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(regs) != 0 {
		t.Errorf("expected no registrations, got %d", len(regs))
	}
}

func TestImageFromArgs(t *testing.T) {
	cases := []struct{ args []string; want string }{
		{[]string{"run", "-i", "--rm", "artifacts.toji.homes/neunexus/gopedia-mcp:latest"}, "artifacts.toji.homes/neunexus/gopedia-mcp:latest"},
		{[]string{"run", "-e", "X", "ghcr.io/foo/bar"}, "ghcr.io/foo/bar"},
		{[]string{"-y", "some-npm-pkg"}, ""}, // not a docker invocation (no registry path)
	}
	for _, c := range cases {
		if got := imageFromArgs(c.args); got != c.want {
			t.Errorf("imageFromArgs(%v) = %q, want %q", c.args, got, c.want)
		}
	}
}
