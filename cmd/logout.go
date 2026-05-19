package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tojiuni/morphso/internal/config"
)

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "로그아웃 (토큰 삭제)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.DefaultLoad()
		if err != nil {
			return err
		}
		cfg.Token = ""
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Println("로그아웃 완료.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(logoutCmd)
}
