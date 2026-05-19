package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tojiuni/morphso/internal/config"
	"github.com/tojiuni/morphso/internal/hub"
)

var infoCmd = &cobra.Command{
	Use:   "info <package>",
	Short: "패키지 상세 정보",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.DefaultLoad()
		if err != nil {
			return err
		}
		if hubURL != "" {
			cfg.HubURL = hubURL
		}
		client := hub.NewClient(cfg.HubURL, cfg.Token)
		pkg, err := client.GetPackage(args[0])
		if errors.Is(err, hub.ErrNotFound) {
			return fmt.Errorf("패키지 '%s'를 찾을 수 없습니다", args[0])
		}
		if err != nil {
			return fmt.Errorf("정보 조회 실패: %w", err)
		}

		verified := ""
		if pkg.Verified {
			verified = " [검증됨]"
		}
		price := "무료"
		if pkg.PriceCents > 0 {
			price = fmt.Sprintf("₩%d", pkg.PriceCents)
		}

		fmt.Printf("이름:       %s%s\n", pkg.Name, verified)
		fmt.Printf("Slug:       %s\n", pkg.Slug)
		fmt.Printf("타입:       %s\n", pkg.Type)
		fmt.Printf("가격:       %s\n", price)
		fmt.Printf("다운로드:   %d\n", pkg.Downloads)
		if len(pkg.Tags) > 0 {
			fmt.Printf("태그:       ")
			for i, t := range pkg.Tags {
				if i > 0 {
					fmt.Print(", ")
				}
				fmt.Print(t)
			}
			fmt.Println()
		}
		if pkg.Description != "" {
			fmt.Printf("설명:       %s\n", pkg.Description)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(infoCmd)
}
