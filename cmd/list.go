package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tojiuni/morphso/internal/config"
	"github.com/tojiuni/morphso/internal/hub"
	"github.com/tojiuni/morphso/internal/localstate"
)

const neunexusRegistry = "artifacts.toji.homes/neunexus"

var listRemote bool

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "설치된 패키지 목록 (기본: 로컬 / --remote: hub 이력)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if listRemote {
			return listRemoteInstalls()
		}
		return listLocal()
	},
}

// listLocal infers installed packages from the system: MCP client
// registrations and the local docker image cache. No login required.
func listLocal() error {
	regs, _ := localstate.MCPRegistrations()
	imgs := localstate.DockerImages(neunexusRegistry)

	if len(regs) == 0 && len(imgs) == 0 {
		fmt.Println("로컬에 설치된 패키지 없음.")
		return nil
	}

	if len(regs) > 0 {
		fmt.Println("MCP 서버 (클라이언트 등록):")
		fmt.Printf("  %-16s %-8s %s\n", "SERVER", "CLIENTS", "IMAGE")
		for _, r := range regs {
			img := r.Image
			if img == "" {
				img = "-"
			}
			fmt.Printf("  %-16s %-8s %s\n", r.Server, fmt.Sprintf("%d개", len(r.Clients)), img)
		}
	}
	if len(imgs) > 0 {
		fmt.Println("\nDocker 이미지:")
		for _, im := range imgs {
			fmt.Printf("  %s\n", im)
		}
	}
	return nil
}

func listRemoteInstalls() error {
	cfg, err := config.DefaultLoad()
	if err != nil {
		return err
	}
	if hubURL != "" {
		cfg.HubURL = hubURL
	}
	if cfg.Token == "" {
		return fmt.Errorf("로그인이 필요합니다. 'morphso login'을 실행하세요.")
	}
	client := hub.NewClient(cfg.HubURL, cfg.Token)
	records, err := client.GetInstalls()
	if errors.Is(err, hub.ErrUnauthorized) {
		return fmt.Errorf("인증 만료. 'morphso login'으로 재로그인하세요.")
	}
	if err != nil {
		return fmt.Errorf("이력 조회 실패: %w", err)
	}
	if len(records) == 0 {
		fmt.Println("설치 이력 없음.")
		return nil
	}
	fmt.Printf("%-20s %-8s %-10s %s\n", "PACKAGE", "STRATEGY", "VERSION", "INSTALLED AT")
	fmt.Println(strings.Repeat("-", 62))
	for _, r := range records {
		version := r.Version
		if version == "" {
			version = "-"
		}
		fmt.Printf("%-20s %-8s %-10s %s\n",
			r.PackageSlug, r.Strategy, version,
			r.InstalledAt.Format("2006-01-02 15:04"))
	}
	return nil
}

func init() {
	listCmd.Flags().BoolVar(&listRemote, "remote", false, "hub 설치 이력 조회 (기본은 로컬)")
	rootCmd.AddCommand(listCmd)
}
