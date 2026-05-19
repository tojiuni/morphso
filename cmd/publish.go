package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var publishCmd = &cobra.Command{
	Use:   "publish",
	Short: "패키지 배포",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("패키지 배포 기능은 아직 구현되지 않았습니다.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(publishCmd)
}
