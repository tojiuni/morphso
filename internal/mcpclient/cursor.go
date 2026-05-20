package mcpclient

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type cursorClient struct{}

func newCursorReal() MCPClient { return &cursorClient{} }

func (c *cursorClient) Name() string { return "cursor" }

func (c *cursorClient) configPath() string {
	return filepath.Join(os.Getenv("HOME"), ".cursor", "mcp.json")
}

func (c *cursorClient) Detected() bool {
	_, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".cursor"))
	return err == nil
}

func (c *cursorClient) ExistingEntry(name string) (*MCPServerEntry, error) {
	cfg, err := readJSONConfig(c.configPath())
	if err != nil {
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

func (c *cursorClient) Register(name string, entry MCPServerEntry) error {
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

func (c *cursorClient) Unregister(name string) error {
	cfg, err := readJSONConfig(c.configPath())
	if err != nil {
		return err
	}
	if cfg == nil {
		return nil
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		return nil
	}
	delete(servers, name)
	cfg["mcpServers"] = servers
	return writeJSONConfigAtomic(c.configPath(), cfg)
}

// --- shared JSON helpers (used by Cursor, Gemini, Claude direct-edit paths) ---

// readJSONConfig returns the parsed JSON map, or nil map if file is absent.
// Returns a "corrupt config" error if file exists but is malformed —
// never silently overwrites a corrupted file.
func readJSONConfig(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("corrupt config at %s: %w (refusing to overwrite)", path, err)
	}
	return cfg, nil
}

// writeJSONConfigAtomic writes the config to a tmp file in the same dir,
// fsyncs implicitly via close, chmods 0600, then renames over the target.
// A best-effort .bak backup is left next to the target.
func writeJSONConfigAtomic(path string, cfg map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if data, err := os.ReadFile(path); err == nil {
		_ = os.WriteFile(path+".bak", data, 0600)
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mcp-*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	tmp.Close()
	if err := os.Chmod(tmp.Name(), 0600); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

func decodeEntry(raw any) (*MCPServerEntry, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var e MCPServerEntry
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, err
	}
	return &e, nil
}
