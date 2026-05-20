package mcpclient

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
)

type claudeCodeClient struct{}

func newClaudeCodeReal() MCPClient { return &claudeCodeClient{} }

func (c *claudeCodeClient) Name() string { return "claude-code" }

func (c *claudeCodeClient) configPath() string {
	return filepath.Join(os.Getenv("HOME"), ".claude.json")
}

// Detected: claude binary in PATH OR ~/.claude.json present.
func (c *claudeCodeClient) Detected() bool {
	if _, err := os.Stat(c.configPath()); err == nil {
		return true
	}
	_, err := exec.LookPath("claude")
	return err == nil
}

func (c *claudeCodeClient) ExistingEntry(name string) (*MCPServerEntry, error) {
	cfg, err := readJSONConfig(c.configPath())
	if err != nil || cfg == nil {
		return nil, err
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		return nil, nil
	}
	raw, ok := servers[name]
	if !ok {
		return nil, nil
	}
	return decodeEntry(raw)
}

func (c *claudeCodeClient) Register(name string, entry MCPServerEntry) error {
	// Prefer the official subprocess so Claude Code handles scope/precedence.
	// Tests set MOSO_DISABLE_CLAUDE_SUBPROCESS=1 to exercise the direct-edit path.
	if os.Getenv("MOSO_DISABLE_CLAUDE_SUBPROCESS") == "" {
		if _, err := exec.LookPath("claude"); err == nil {
			if err := c.registerViaCLI(name, entry); err == nil {
				return nil
			}
			// On subprocess failure, fall through to direct edit.
		}
	}
	return c.registerDirect(name, entry)
}

func (c *claudeCodeClient) registerViaCLI(name string, entry MCPServerEntry) error {
	blob, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	cmd := exec.Command("claude", "mcp", "add-json", name, string(blob), "--scope", "user")
	cmd.Stdout = os.Stderr // surface to user; do not mix with moso stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (c *claudeCodeClient) registerDirect(name string, entry MCPServerEntry) error {
	cfg, err := readJSONConfig(c.configPath())
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers[name] = entry
	cfg["mcpServers"] = servers
	return writeJSONConfigAtomic(c.configPath(), cfg)
}

func (c *claudeCodeClient) Unregister(name string) error {
	if os.Getenv("MOSO_DISABLE_CLAUDE_SUBPROCESS") == "" {
		if _, err := exec.LookPath("claude"); err == nil {
			cmd := exec.Command("claude", "mcp", "remove", name, "--scope", "user")
			cmd.Stdout = os.Stderr
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err == nil {
				return nil
			}
			// fall through
		}
	}
	cfg, err := readJSONConfig(c.configPath())
	if err != nil || cfg == nil {
		return err
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		return nil
	}
	delete(servers, name)
	cfg["mcpServers"] = servers
	return writeJSONConfigAtomic(c.configPath(), cfg)
}
