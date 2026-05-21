package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tojiuni/morphso/internal/config"
	"github.com/tojiuni/morphso/internal/hub"
	"github.com/tojiuni/morphso/internal/localstate"
	"github.com/tojiuni/morphso/internal/mcpclient"
)

var (
	removeRemote bool
	removeYes    bool
)

// dockerNameRe extracts the container name from a docker-strategy install
// script (`docker run ... --name <name> ...`).
var dockerNameRe = regexp.MustCompile(`--name[=\s]+(\S+)`)

var removeCmd = &cobra.Command{
	Use:   "remove <package>",
	Short: "패키지 제거 (기본: 로컬 정리 / --remote: hub 레지스트리 삭제)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if removeRemote {
			return runRemoveRemote(args[0])
		}
		return runRemoveLocal(args[0])
	},
}

// purgeLocalFootprint undoes a single package's local install: unregister its
// MCP server (if any), remove its docker container (parsed from the docker
// install script's --name), and delete its package-specific (neunexus) images.
// Cached public base images (postgres, qdrant, …) are preserved.
func purgeLocalFootprint(client *hub.Client, slug string, out io.Writer) {
	pkg, _ := client.GetPackage(slug) // best-effort; works offline-degraded

	// MCP client 등록 해제 (mcp 패키지일 때만, 멱등).
	unregisterFromClients(pkg, mcpclient.DetectInstalled(), out)

	// docker 컨테이너: docker-strategy 설치 스크립트의 --name 을 파싱해 제거.
	if s, err := client.GetInstallScript(slug, "latest", "docker"); err == nil && s != nil {
		if m := dockerNameRe.FindStringSubmatch(s.Script); m != nil {
			for _, a := range localstate.DockerRemoveContainer(m[1]) {
				fmt.Fprintf(out, "  ✓ %s\n", a)
			}
		}
	}

	// 패키지 전용(neunexus) 이미지만 제거 — 캐시된 public 베이스 이미지는 보존.
	images := map[string]bool{neunexusRegistry + "/" + slug: true}
	if pkg != nil && pkg.MCPMetadata != nil && pkg.MCPMetadata.Docker != nil && pkg.MCPMetadata.Docker.Image != "" {
		images[pkg.MCPMetadata.Docker.Image] = true
	}
	for img := range images {
		for _, a := range localstate.DockerPurge(img) {
			fmt.Fprintf(out, "  ✓ %s\n", a)
		}
	}
}

// runRemoveLocal undoes a local install (MCP unregister + docker
// container/image) and, when the package declares dependencies, offers to clean
// those up too. The hub registry entry is left intact (use --remote to delete).
func runRemoveLocal(slug string) error {
	cfg, _ := config.DefaultLoad()
	if hubURL != "" {
		cfg.HubURL = hubURL
	}
	client := hub.NewClient(cfg.HubURL, cfg.Token)

	fmt.Printf("'%s' 로컬 정리 중...\n", slug)
	purgeLocalFootprint(client, slug, os.Stdout)

	// Forward dependencies → offer to clean them up too.
	if dr, err := client.GetDependencies(slug); err == nil && dr != nil && len(dr.Dependencies) > 0 {
		fmt.Printf("\n'%s'가 의존하는 패키지:\n", slug)
		for _, d := range dr.Dependencies {
			kind := "필수"
			if d.Optional {
				kind = "optional"
			}
			fmt.Printf("  - %s (%s)\n", d.Package.Slug, kind)
		}
		confirm := removeYes
		if !confirm {
			fmt.Print("이 의존성들도 함께 로컬 정리할까요? [y/N] ")
			ans, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			a := strings.TrimSpace(strings.ToLower(ans))
			confirm = a == "y" || a == "yes"
		}
		if confirm {
			for _, d := range dr.Dependencies {
				fmt.Printf("\n— 의존성 '%s' 정리\n", d.Package.Slug)
				purgeLocalFootprint(client, d.Package.Slug, os.Stdout)
			}
		} else {
			fmt.Println("의존성은 유지합니다.")
		}
	}

	fmt.Printf("\n'%s' 로컬 정리 완료. (hub 레지스트리는 유지됨 — 전역 삭제는 --remote)\n", slug)
	return nil
}

// runRemoveRemote deletes the package from the hub registry (author/admin
// only) and then unregisters it locally. This is destructive and global.
func runRemoveRemote(slug string) error {
	cfg, err := config.DefaultLoad()
	if err != nil {
		return err
	}
	if hubURL != "" {
		cfg.HubURL = hubURL
	}
	client := hub.NewClient(cfg.HubURL, cfg.Token)

	pkg, getErr := client.GetPackage(slug)

	conflict, err := client.DeletePackage(slug, false, false)
	if err == nil {
		fmt.Printf("'%s' 패키지가 hub에서 삭제되었습니다.\n", slug)
		if pkg != nil && getErr == nil {
			unregisterFromClients(pkg, mcpclient.DetectInstalled(), os.Stdout)
		}
		return nil
	}
	if !errors.Is(err, hub.ErrDeleteConflict) {
		return err
	}

	fmt.Printf("'%s' 패키지는 다음 패키지들의 의존성입니다:\n", slug)
	for _, d := range conflict.Dependents {
		fmt.Printf("  - %s\n", d)
	}
	fmt.Println()
	fmt.Println("어떻게 처리할까요?")
	fmt.Printf("  [1] '%s' 및 이에 의존하는 패키지 모두 삭제 (cascade)\n", slug)
	fmt.Printf("  [2] '%s'만 삭제 (의존 패키지의 목록에서 제거)\n", slug)
	fmt.Println("  [3] 취소")
	fmt.Print("> ")

	reader := bufio.NewReader(os.Stdin)
	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)

	switch choice {
	case "1":
		_, err = client.DeletePackage(slug, true, false)
		if err != nil {
			return fmt.Errorf("cascade 삭제 실패: %w", err)
		}
		fmt.Printf("'%s' 및 의존 패키지가 모두 삭제되었습니다.\n", slug)
		if pkg != nil && getErr == nil {
			unregisterFromClients(pkg, mcpclient.DetectInstalled(), os.Stdout)
		}
	case "2":
		_, err = client.DeletePackage(slug, false, true)
		if err != nil {
			return fmt.Errorf("삭제 실패: %w", err)
		}
		fmt.Printf("'%s' 패키지가 삭제되었습니다.\n", slug)
		if pkg != nil && getErr == nil {
			unregisterFromClients(pkg, mcpclient.DetectInstalled(), os.Stdout)
		}
	default:
		fmt.Println("취소되었습니다.")
	}
	return nil
}

func init() {
	removeCmd.Flags().BoolVar(&removeRemote, "remote", false, "hub 레지스트리에서 전역 삭제 (기본은 로컬 정리)")
	removeCmd.Flags().BoolVarP(&removeYes, "yes", "y", false, "의존성 정리 프롬프트 자동 승인")
	rootCmd.AddCommand(removeCmd)
}
