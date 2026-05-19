package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tojiuni/morphso/internal/config"
)

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "현재 로그인 상태 표시",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.DefaultLoad()
		if err != nil {
			return err
		}
		if cfg.Token == "" {
			fmt.Println("로그인되지 않음. 'morphso login'으로 로그인하세요.")
			return nil
		}
		fmt.Println("로그인됨 (토큰 있음). 자세한 정보: morphso-hub /users/me 미구현.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(whoamiCmd)
}
