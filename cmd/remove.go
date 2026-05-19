package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tojiuni/morphso/internal/config"
	"github.com/tojiuni/morphso/internal/hub"
)

var removeCmd = &cobra.Command{
	Use:   "remove <package>",
	Short: "패키지 제거",
	Args:  cobra.ExactArgs(1),
	RunE:  runRemove,
}

func init() {
	rootCmd.AddCommand(removeCmd)
}

func runRemove(cmd *cobra.Command, args []string) error {
	slug := args[0]
	cfg, err := config.DefaultLoad()
	if err != nil {
		return err
	}
	client := hub.NewClient(cfg.HubURL, cfg.Token)

	conflict, err := client.DeletePackage(slug, false, false)
	if err == nil {
		fmt.Printf("'%s' 패키지가 삭제되었습니다.\n", slug)
		return nil
	}
	if !errors.Is(err, hub.ErrDeleteConflict) {
		return err
	}

	// conflict: show dependents, ask user
	fmt.Printf("'%s' 패키지는 다음 패키지들의 의존성입니다:\n", slug)
	for _, d := range conflict.Dependents {
		fmt.Printf("  - %s\n", d)
	}
	fmt.Println()
	fmt.Println("어떻게 처리할까요?")
	fmt.Printf("  [1] '%s' 및 이에 의존하는 패키지 모두 삭제 (cascade)\n", slug)
	fmt.Printf("  [2] '%s'만 삭제 (의존 패키지의 목록에서 제거)\n", slug)
	fmt.Println("  [3] 취소")
	fmt.Print("> ")

	reader := bufio.NewReader(os.Stdin)
	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)

	switch choice {
	case "1":
		_, err = client.DeletePackage(slug, true, false)
		if err != nil {
			return fmt.Errorf("cascade 삭제 실패: %w", err)
		}
		fmt.Printf("'%s' 및 의존 패키지가 모두 삭제되었습니다.\n", slug)
	case "2":
		_, err = client.DeletePackage(slug, false, true)
		if err != nil {
			return fmt.Errorf("삭제 실패: %w", err)
		}
		fmt.Printf("'%s' 패키지가 삭제되었습니다.\n", slug)
	default:
		fmt.Println("취소되었습니다.")
	}
	return nil
}
