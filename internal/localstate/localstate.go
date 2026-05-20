// Package localstate infers which morphso packages are present on the local
// machine, without a local manifest: it reads MCP client config files and
// queries the local docker image cache. This backs the local-first behavior
// of `morphso list`, `search`, and `remove`.
package localstate

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Registration is a single MCP server seen across one or more MCP clients.
type Registration struct {
	Server  string   // MCP server name as written in client configs
	Image   string   // docker image from args, "" if not a docker-run server
	Clients []string // client names where this server is registered
}

// mcpSource describes where one client stores its server map.
type mcpSource struct {
	client string
	path   string   // relative to $HOME
	keys   []string // nested path to the server map
}

func sources() []mcpSource {
	return []mcpSource{
		{"claude-code", ".claude.json", []string{"mcpServers"}},
		{"cursor", filepath.Join(".cursor", "mcp.json"), []string{"mcpServers"}},
		{"gemini-cli", filepath.Join(".gemini", "settings.json"), []string{"mcp", "servers"}},
	}
}

// MCPRegistrations scans all known MCP client configs under $HOME and returns
// one Registration per server name, merging the clients it appears in.
func MCPRegistrations() ([]Registration, error) {
	home := os.Getenv("HOME")
	merged := map[string]*Registration{}

	for _, s := range sources() {
		servers := readServerMap(filepath.Join(home, s.path), s.keys)
		for name, raw := range servers {
			entry, _ := raw.(map[string]any)
			reg, ok := merged[name]
			if !ok {
				reg = &Registration{Server: name}
				merged[name] = reg
			}
			reg.Clients = append(reg.Clients, s.client)
			// Only docker-run servers carry an image; other commands (npx,
			// postgres-mcp, …) may have URL-like args that are not images.
			if reg.Image == "" && entry["command"] == "docker" {
				reg.Image = imageFromArgs(toStrings(entry["args"]))
			}
		}
	}

	out := make([]Registration, 0, len(merged))
	for _, r := range merged {
		sort.Strings(r.Clients)
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Server < out[j].Server })
	return out, nil
}

// readServerMap loads path and walks keys to the server map. Missing file or
// missing keys yield an empty map (not an error) — absence is normal.
func readServerMap(path string, keys []string) map[string]any {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg map[string]any
	if json.Unmarshal(b, &cfg) != nil {
		return nil
	}
	node := any(cfg)
	for _, k := range keys {
		m, ok := node.(map[string]any)
		if !ok {
			return nil
		}
		node = m[k]
	}
	servers, _ := node.(map[string]any)
	return servers
}

// imageFromArgs returns the last registry-qualified image reference in args
// (e.g. "artifacts.toji.homes/neunexus/gopedia-mcp:latest"), or "" if none —
// distinguishing docker-run MCP servers from npx/binary ones.
func imageFromArgs(args []string) string {
	image := ""
	for _, a := range args {
		if strings.HasPrefix(a, "-") || !strings.Contains(a, "/") {
			continue
		}
		host := a[:strings.Index(a, "/")]
		if strings.Contains(host, ".") || strings.Contains(host, ":") {
			image = a // keep last match
		}
	}
	return image
}

func toStrings(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, x := range raw {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// DockerImages returns local docker image references under prefix
// (e.g. "artifacts.toji.homes/neunexus"). Returns nil if docker is absent.
func DockerImages(prefix string) []string {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil
	}
	out, err := exec.Command("docker", "images", "--format", "{{.Repository}}:{{.Tag}}").Output()
	if err != nil {
		return nil
	}
	var imgs []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && strings.HasPrefix(line, prefix) {
			imgs = append(imgs, line)
		}
	}
	sort.Strings(imgs)
	return imgs
}

// DockerPurge removes any containers created from image (matched by ancestor)
// and then deletes all local tags of the image. repo may be given with or
// without a tag. Returns the human-readable actions taken. No-op if docker is
// absent or nothing matches.
func DockerPurge(repo string) []string {
	var actions []string
	if _, err := exec.LookPath("docker"); err != nil {
		return actions
	}
	repo = strings.TrimSuffix(repo, ":latest")
	if cids := dockerLines("ps", "-aq", "--filter", "ancestor="+repo); len(cids) > 0 {
		_ = exec.Command("docker", append([]string{"rm", "-f"}, cids...)...).Run()
		actions = append(actions, "컨테이너 제거: "+repo)
	}
	ids := uniq(dockerLines("images", "-q", repo))
	if len(ids) > 0 {
		if exec.Command("docker", append([]string{"rmi", "-f"}, ids...)...).Run() == nil {
			actions = append(actions, "이미지 제거: "+repo)
		}
	}
	return actions
}

func dockerLines(args ...string) []string {
	out, err := exec.Command("docker", args...).Output()
	if err != nil {
		return nil
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
