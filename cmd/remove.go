package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tojiuni/morphso/internal/config"
	"github.com/tojiuni/morphso/internal/hub"
	"github.com/tojiuni/morphso/internal/localstate"
	"github.com/tojiuni/morphso/internal/mcpclient"
)

var removeRemote bool

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

// runRemoveLocal undoes a local install: it unregisters the MCP server from
// detected clients and deletes the package's docker image(s). The hub
// registry entry is left intact (use --remote to delete from the hub).
//
// Footprint is resolved from hub metadata when reachable (exact MCP server
// name + image); otherwise it falls back to a slug-based heuristic.
func runRemoveLocal(slug string) error {
	cfg, _ := config.DefaultLoad()
	if hubURL != "" {
		cfg.HubURL = hubURL
	}
	client := hub.NewClient(cfg.HubURL, cfg.Token)
	pkg, getErr := client.GetPackage(slug) // best-effort; works offline-degraded

	fmt.Printf("'%s' 로컬 정리 중...\n", slug)

	// 1. MCP client 등록 해제 (mcp 패키지일 때만, 멱등).
	unregisterFromClients(pkg, mcpclient.DetectInstalled(), os.Stdout)

	// 2. docker 이미지 제거 — 메타데이터 이미지 + slug 휴리스틱.
	images := map[string]bool{}
	if pkg != nil && pkg.MCPMetadata != nil && pkg.MCPMetadata.Docker != nil && pkg.MCPMetadata.Docker.Image != "" {
		images[pkg.MCPMetadata.Docker.Image] = true
	}
	images[neunexusRegistry+"/"+slug] = true

	var actions []string
	for img := range images {
		actions = append(actions, localstate.DockerPurge(img)...)
	}
	for _, a := range actions {
		fmt.Printf("✓ %s\n", a)
	}

	if getErr != nil {
		fmt.Printf("· hub 메타데이터 조회 실패 — slug 휴리스틱으로만 정리했습니다 (%v)\n", getErr)
	}
	if len(actions) == 0 {
		fmt.Printf("· 제거할 docker 이미지 없음 (이미 정리됨이거나 다른 이미지명일 수 있음)\n")
	}
	fmt.Printf("'%s' 로컬 정리 완료. (hub 레지스트리는 유지됨 — 전역 삭제는 --remote)\n", slug)
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
	rootCmd.AddCommand(removeCmd)
}
