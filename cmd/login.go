package cmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/tojiuni/morphso/internal/auth"
	"github.com/tojiuni/morphso/internal/config"
)

const zitadelIssuer = "https://auth.toji.homes"
const zitadelClientID = "373499369619582167"

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "ZITADEL Device Flow로 로그인",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.DefaultLoad()
		if err != nil {
			return err
		}

		flow := auth.NewDeviceFlow(zitadelIssuer, zitadelClientID)
		resp, err := flow.Start()
		if err != nil {
			return fmt.Errorf("인증 시작 실패: %w", err)
		}

		fmt.Printf("\n브라우저에서 다음 URL로 이동하세요:\n  %s\n", resp.VerificationURI)
		fmt.Printf("\n코드를 입력하세요: %s\n\n", resp.UserCode)
		fmt.Print("인증 완료를 기다리는 중")

		interval := time.Duration(resp.Interval) * time.Second
		if interval == 0 {
			interval = 5 * time.Second
		}
		deadline := time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)

		for time.Now().Before(deadline) {
			time.Sleep(interval)
			fmt.Print(".")

			token, err := flow.PollOnce(resp.DeviceCode)
			if errors.Is(err, auth.ErrAuthorizationPending) || errors.Is(err, auth.ErrSlowDown) {
				continue
			}
			if err != nil {
				return fmt.Errorf("\n인증 실패: %w", err)
			}

			cfg.Token = token
			if err := cfg.Save(); err != nil {
				return fmt.Errorf("토큰 저장 실패: %w", err)
			}
			fmt.Println("\n\n✓ 로그인 성공!")
			return nil
		}
		return fmt.Errorf("인증 시간 초과")
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
}
