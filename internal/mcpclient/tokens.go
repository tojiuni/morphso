package mcpclient

import (
	"fmt"
	"regexp"
)

// TokenContext carries the values available for template expansion in
// MCP command args. See ExpandTokens for the supported namespace.
type TokenContext struct {
	Image   string
	Version string
	Slug    string
}

var tokenPattern = regexp.MustCompile(`\{\{([a-zA-Z0-9_:-]+)\}\}`)

// ExpandTokens substitutes recognized {{...}} tokens in each arg. Supported:
//   {{image}}   → "<image>:<version>" (or "<image>" if version empty)
//   {{version}} → version
//   {{slug}}    → slug
// {{env:NAME}} is intentionally NOT expanded here; the hub validator
// forbids it from appearing in args (would leak env values via process args).
// Any unknown token results in an error.
func ExpandTokens(args []string, ctx TokenContext) ([]string, error) {
	out := make([]string, len(args))
	for i, a := range args {
		expanded, err := expand(a, ctx)
		if err != nil {
			return nil, err
		}
		out[i] = expanded
	}
	return out, nil
}

func expand(s string, ctx TokenContext) (string, error) {
	var firstErr error
	result := tokenPattern.ReplaceAllStringFunc(s, func(match string) string {
		name := match[2 : len(match)-2]
		switch name {
		case "image":
			if ctx.Version == "" {
				return ctx.Image
			}
			return ctx.Image + ":" + ctx.Version
		case "version":
			return ctx.Version
		case "slug":
			return ctx.Slug
		default:
			if firstErr == nil {
				firstErr = fmt.Errorf("unknown token %s", match)
			}
			return match
		}
	})
	return result, firstErr
}
