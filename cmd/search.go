package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tojiuni/morphso/internal/config"
	"github.com/tojiuni/morphso/internal/hub"
	"github.com/tojiuni/morphso/internal/localstate"
)

var searchRemote bool

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "패키지 검색 (기본: 로컬 설치본 / --remote: hub 카탈로그)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if searchRemote {
			return searchRemoteCatalog(args[0])
		}
		return searchLocal(args[0])
	},
}

// searchLocal filters locally-detected packages (MCP servers + docker images)
// by a case-insensitive substring of the query. No login required.
func searchLocal(query string) error {
	q := strings.ToLower(query)
	regs, _ := localstate.MCPRegistrations()
	imgs := localstate.DockerImages(neunexusRegistry)

	var rows [][2]string // (name, detail)
	for _, r := range regs {
		if strings.Contains(strings.ToLower(r.Server), q) || strings.Contains(strings.ToLower(r.Image), q) {
			rows = append(rows, [2]string{r.Server, "mcp · " + r.Image})
		}
	}
	for _, im := range imgs {
		if strings.Contains(strings.ToLower(im), q) {
			rows = append(rows, [2]string{im, "docker image"})
		}
	}

	if len(rows) == 0 {
		fmt.Printf("로컬에 '%s' 일치 항목 없음 (hub 검색: --remote)\n", query)
		return nil
	}
	fmt.Printf("%-28s %s\n", "NAME", "KIND")
	fmt.Println(strings.Repeat("-", 50))
	for _, row := range rows {
		fmt.Printf("%-28s %s\n", row[0], row[1])
	}
	return nil
}

func searchRemoteCatalog(query string) error {
	cfg, err := config.DefaultLoad()
	if err != nil {
		return err
	}
	if hubURL != "" {
		cfg.HubURL = hubURL
	}
	client := hub.NewClient(cfg.HubURL, cfg.Token)
	pkgs, err := client.Search(query, 20, 0)
	if err != nil {
		return fmt.Errorf("검색 실패: %w", err)
	}
	if len(pkgs) == 0 {
		fmt.Printf("'%s'에 대한 결과 없음\n", query)
		return nil
	}
	fmt.Printf("%-20s %-8s %-6s %s\n", "NAME", "TYPE", "PRICE", "DESCRIPTION")
	fmt.Println(strings.Repeat("-", 60))
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
}

func init() {
	searchCmd.Flags().BoolVar(&searchRemote, "remote", false, "hub 카탈로그 검색 (기본은 로컬)")
	rootCmd.AddCommand(searchCmd)
}
