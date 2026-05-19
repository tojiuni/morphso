package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tojiuni/morphso/internal/config"
	"github.com/tojiuni/morphso/internal/hub"
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "패키지 검색",
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
		pkgs, err := client.Search(args[0], 20, 0)
		if err != nil {
			return fmt.Errorf("검색 실패: %w", err)
		}
		if len(pkgs) == 0 {
			fmt.Printf("'%s'에 대한 결과 없음\n", args[0])
			return nil
		}
		fmt.Printf("%-20s %-8s %-6s %s\n", "NAME", "TYPE", "PRICE", "DESCRIPTION")
		fmt.Println("------------------------------------------------------------")
		for _, p := range pkgs {
			price := "무료"
			if p.PriceCents > 0 {
				price = fmt.Sprintf("₩%d", p.PriceCents)
			}
			verified := ""
			if p.Verified {
				verified = " ✓"
			}
			fmt.Printf("%-20s %-8s %-6s %s%s\n", p.Slug, p.Type, price, p.Description, verified)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(searchCmd)
}
