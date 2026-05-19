package hub

import "strings"

// ParseSlugVersion splits "gopedia@1.2.3" → ("gopedia", "1.2.3").
// Returns ("slug", "latest") when no @ suffix is present.
func ParseSlugVersion(arg string) (slug, version string) {
	if idx := strings.LastIndex(arg, "@"); idx != -1 {
		return arg[:idx], arg[idx+1:]
	}
	return arg, "latest"
}
