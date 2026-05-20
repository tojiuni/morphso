package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"golang.org/x/term"

	"github.com/tojiuni/morphso/internal/hub"
	"github.com/tojiuni/morphso/internal/mcpclient"
)

// runMCPRegistration is the production entry point. It detects installed MCP
// clients and registers the package's server with each one.
// runMCPRegistrationWithClients is the testable core that takes an explicit
// client list (for fakes).
func runMCPRegistration(pkg *hub.Package, strategy, version string, reader *bufio.Reader, out io.Writer, yes, reconfigure bool) error {
	clients := mcpclient.DetectInstalled()
	return runMCPRegistrationWithClients(pkg, strategy, version, reader, out, clients, yes, reconfigure)
}

func runMCPRegistrationWithClients(
	pkg *hub.Package, strategy, version string,
	reader *bufio.Reader, out io.Writer,
	clients []mcpclient.MCPClient,
	yes, reconfigure bool,
) error {
	meta := pkg.MCPMetadata
	if meta == nil {
		return fmt.Errorf("mcp package missing mcp_metadata")
	}
	if meta.Transport != "" && meta.Transport != "stdio" {
		return fmt.Errorf("transport %q not supported in v1 (stdio only)", meta.Transport)
	}
	if runtime.GOOS == "windows" {
		fmt.Fprintln(out, "windows registration not implemented in v1; skipping MCP client registration")
		return nil
	}

	resolved := applyOverride(meta, runtime.GOOS)

	// Gather existing env from any detected client (first hit wins) so we can
	// offer the user's previous values as defaults on reinstall.
	existing := map[string]string{}
	if !reconfigure {
		for _, c := range clients {
			e, err := c.ExistingEntry(meta.ServerName)
			if err != nil || e == nil {
				continue
			}
			for k, v := range e.Env {
				if _, ok := existing[k]; !ok {
					existing[k] = v
				}
			}
		}
	}

	collected, err := collectEnv(meta.EnvSchema, existing, reader, out, yes)
	if err != nil {
		return err
	}

	entry, err := buildEntry(resolved, strategy, version, pkg.Slug, collected)
	if err != nil {
		return err
	}

	for _, c := range clients {
		if !c.Detected() {
			fmt.Fprintf(out, "· %s: 미감지, skip\n", c.Name())
			continue
		}
		if err := c.Register(meta.ServerName, entry); err != nil {
			fmt.Fprintf(out, "✗ %s: 등록 실패: %v\n", c.Name(), err)
			continue
		}
		fmt.Fprintf(out, "✓ %s에 '%s' 등록\n", c.Name(), meta.ServerName)
	}
	return nil
}

// applyOverride returns a shallow copy of m with any GOOS-specific
// Native/Docker overrides applied. Returns m unchanged when no override exists.
func applyOverride(m *hub.MCPMetadata, goos string) *hub.MCPMetadata {
	if m.PlatformOverrides == nil {
		return m
	}
	ov, ok := m.PlatformOverrides[goos]
	if !ok {
		return m
	}
	resolved := *m
	if ov.Native != nil {
		resolved.Native = ov.Native
	}
	if ov.Docker != nil {
		resolved.Docker = ov.Docker
	}
	return &resolved
}

// collectEnv prompts for each env_schema entry. In --yes mode it falls back
// to os.Getenv then schema Default. Existing values (from a prior install)
// are offered as defaults during interactive prompts.
func collectEnv(schema []hub.MCPEnvSpec, existing map[string]string, reader *bufio.Reader, out io.Writer, yes bool) (map[string]string, error) {
	collected := map[string]string{}
	for _, e := range schema {
		defaultVal := existing[e.Name]
		if defaultVal == "" {
			defaultVal = e.Default
		}
		if yes {
			val := os.Getenv(e.Name)
			if val == "" {
				val = defaultVal
			}
			if val == "" && e.Required {
				return nil, fmt.Errorf("required env %s not provided (--yes mode)", e.Name)
			}
			if val != "" {
				collected[e.Name] = val
			}
			continue
		}
		display := defaultVal
		if e.Secret && display != "" {
			display = "***"
		}
		prompt := e.Prompt
		if prompt == "" {
			prompt = e.Name
		}
		if defaultVal != "" {
			fmt.Fprintf(out, "%s [기본: %s]: ", prompt, display)
		} else {
			fmt.Fprintf(out, "%s: ", prompt)
		}
		raw, err := readEnvInput(reader, e.Secret)
		if err != nil {
			return nil, err
		}
		if raw == "" {
			raw = defaultVal
		}
		if raw == "" && e.Required {
			return nil, fmt.Errorf("required env %s not provided", e.Name)
		}
		if raw != "" {
			collected[e.Name] = raw
		}
	}
	return collected, nil
}

// readEnvInput reads one line of input. For secret fields, it disables terminal
// echo via term.ReadPassword when stdin is a tty; otherwise it reads from the
// supplied reader (so tests with piped stdin still work).
func readEnvInput(reader *bufio.Reader, secret bool) (string, error) {
	if secret && term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		if err != nil {
			return "", err
		}
		fmt.Fprintln(os.Stderr) // newline after no-echo input
		return strings.TrimRight(string(b), "\r\n"), nil
	}
	raw, _ := reader.ReadString('\n')
	return strings.TrimRight(raw, "\r\n"), nil
}

// buildEntry constructs the MCPServerEntry for the chosen strategy
// (native or docker) and expands {{...}} tokens in args.
func buildEntry(m *hub.MCPMetadata, strategy, version, slug string, env map[string]string) (mcpclient.MCPServerEntry, error) {
	ctx := mcpclient.TokenContext{Version: version, Slug: slug}
	switch strategy {
	case "native":
		if m.Native == nil {
			return mcpclient.MCPServerEntry{}, fmt.Errorf("native spec missing")
		}
		args, err := mcpclient.ExpandTokens(m.Native.Args, ctx)
		if err != nil {
			return mcpclient.MCPServerEntry{}, err
		}
		return mcpclient.MCPServerEntry{Command: m.Native.Command, Args: args, Env: env}, nil
	case "docker":
		if m.Docker == nil {
			return mcpclient.MCPServerEntry{}, fmt.Errorf("docker spec missing")
		}
		ctx.Image = m.Docker.Image
		args, err := mcpclient.ExpandTokens(m.Docker.Args, ctx)
		if err != nil {
			return mcpclient.MCPServerEntry{}, err
		}
		return mcpclient.MCPServerEntry{Command: "docker", Args: args, Env: env}, nil
	default:
		return mcpclient.MCPServerEntry{}, fmt.Errorf("unsupported strategy %q for mcp", strategy)
	}
}

// errorIfMCPFallbackUnsupported guards the install-script fallback path.
// MCP + docker requires hub-provided install scripts; the local fallback
// (BuildCommand) produces a wrong/daemon-style docker command for stdio MCP.
func errorIfMCPFallbackUnsupported(pkgType, strategy string) error {
	if pkgType == "mcp" && strategy == "docker" {
		return fmt.Errorf("mcp 타입 + docker 전략은 hub install script가 필요합니다 (fallback 경로 미지원)")
	}
	return nil
}
