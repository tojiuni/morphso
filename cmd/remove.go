package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var removeCmd = &cobra.Command{
	Use:   "remove <package>",
	Short: "패키지 제거",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("'%s' 제거 기능은 아직 구현되지 않았습니다.\n", args[0])
		return nil
	},
}

func init() {
	rootCmd.AddCommand(removeCmd)
}
