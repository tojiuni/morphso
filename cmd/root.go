package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	hubURL string
)

var rootCmd = &cobra.Command{
	Use:   "morphso",
	Short: "AI service marketplace CLI",
	Long:  "morphso — AI 전용 서비스 마켓플레이스 CLI. 패키지를 검색하고 AI 추천 방식으로 설치합니다.",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&hubURL, "hub-url", "", "morphso-hub URL override (기본값: config.yaml의 hub_url)")
}
