package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tojiuni/morphso/internal/config"
	"github.com/tojiuni/morphso/internal/hub"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "설치된 패키지 목록 (hub 이력 기반)",
	RunE: func(cmd *cobra.Command, args []string) error {
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
		fmt.Println("--------------------------------------------------------------")
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
	},
}

func init() {
	rootCmd.AddCommand(listCmd)
}
