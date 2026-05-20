package cmd

import (
	"fmt"
	"io"

	"github.com/tojiuni/morphso/internal/hub"
	"github.com/tojiuni/morphso/internal/mcpclient"
)

// unregisterFromClients removes the package's MCP server entry from each
// detected client. Idempotent — undetected clients are skipped silently.
// Non-mcp packages or packages without metadata are no-ops.
func unregisterFromClients(pkg *hub.Package, clients []mcpclient.MCPClient, out io.Writer) {
	if pkg == nil || pkg.Type != "mcp" || pkg.MCPMetadata == nil {
		return
	}
	name := pkg.MCPMetadata.ServerName
	for _, c := range clients {
		if !c.Detected() {
			continue
		}
		if err := c.Unregister(name); err != nil {
			fmt.Fprintf(out, "⚠ %s unregister 실패: %v\n", c.Name(), err)
			continue
		}
		fmt.Fprintf(out, "✓ %s에서 '%s' 제거\n", c.Name(), name)
	}
}
