package installer

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

const artifactBase = "artifacts.toji.homes"

// BuildCommand returns the shell command slice for the given package type + strategy.
func BuildCommand(pkgType, strategy, slug, version string) []string {
	switch strategy {
	case "native":
		return buildNative(pkgType, slug, version)
	case "docker":
		image := artifactBase + "/" + slug
		if version != "" {
			image += ":" + version
		} else {
			image += ":latest"
		}
		return []string{"docker", "run", "-d", "--name", slug, image}
	case "k8s", "helm":
		args := []string{"helm", "install", slug, "morphso/" + slug}
		if version != "" {
			args = append(args, "--version", version)
		}
		return args
	default:
		return buildNative(pkgType, slug, version)
	}
}

func buildNative(pkgType, slug, version string) []string {
	switch pkgType {
	case "npm":
		pkg := slug
		if version != "" {
			pkg += "@" + version
		}
		return []string{"npm", "install", "-g", pkg}
	case "brew":
		return []string{"brew", "install", slug}
	case "pip", "mcp", "recipe", "binary", "helm":
		fallthrough
	default:
		pkg := slug
		if version != "" {
			pkg += "==" + version
		}
		return []string{"pip", "install", pkg}
	}
}

// CheckPrerequisite returns true if the tool is available in PATH.
func CheckPrerequisite(tool string) bool {
	_, err := exec.LookPath(tool)
	return err == nil
}

// Run executes the command, streaming stdout/stderr to the given writer.
func Run(command []string, out io.Writer) error {
	if len(command) == 0 {
		return fmt.Errorf("empty command")
	}
	if out == nil {
		out = os.Stdout
	}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("command failed: %w", err)
	}
	return nil
}
