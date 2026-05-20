package mcpclient

import (
	"os"
	"os/exec"
	"path/filepath"
)

type geminiCLIClient struct{}

func newGeminiCLIReal() MCPClient { return &geminiCLIClient{} }

func (g *geminiCLIClient) Name() string { return "gemini-cli" }

func (g *geminiCLIClient) configPath() string {
	return filepath.Join(os.Getenv("HOME"), ".gemini", "settings.json")
}

func (g *geminiCLIClient) Detected() bool {
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".gemini")); err == nil {
		return true
	}
	_, err := exec.LookPath("gemini")
	return err == nil
}

func (g *geminiCLIClient) ExistingEntry(name string) (*MCPServerEntry, error) {
	cfg, err := readJSONConfig(g.configPath())
	if err != nil {
		return nil, err
	}
	servers := g.serversMap(cfg)
	if servers == nil {
		return nil, nil
	}
	raw, ok := servers[name]
	if !ok {
		return nil, nil
	}
	return decodeEntry(raw)
}

func (g *geminiCLIClient) Register(name string, entry MCPServerEntry) error {
	cfg, err := readJSONConfig(g.configPath())
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	mcp, _ := cfg["mcp"].(map[string]any)
	if mcp == nil {
		mcp = map[string]any{}
	}
	servers, _ := mcp["servers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers[name] = entry
	mcp["servers"] = servers
	cfg["mcp"] = mcp
	return writeJSONConfigAtomic(g.configPath(), cfg)
}

func (g *geminiCLIClient) Unregister(name string) error {
	cfg, err := readJSONConfig(g.configPath())
	if err != nil {
		return err
	}
	if cfg == nil {
		return nil
	}
	servers := g.serversMap(cfg)
	if servers == nil {
		return nil
	}
	delete(servers, name)
	mcp, _ := cfg["mcp"].(map[string]any)
	mcp["servers"] = servers
	cfg["mcp"] = mcp
	return writeJSONConfigAtomic(g.configPath(), cfg)
}

func (g *geminiCLIClient) serversMap(cfg map[string]any) map[string]any {
	mcp, _ := cfg["mcp"].(map[string]any)
	if mcp == nil {
		return nil
	}
	s, _ := mcp["servers"].(map[string]any)
	return s
}
