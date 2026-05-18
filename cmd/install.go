package cmd

import (
	"bufio"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tojiuni/morphso/internal/config"
	"github.com/tojiuni/morphso/internal/hub"
	"github.com/tojiuni/morphso/internal/installer"
	"github.com/tojiuni/morphso/internal/spec"
)

var (
	installStrategy string
	installYes      bool
	installNative   bool
	installDocker   bool
	installK8s      bool
	installHelm     bool
	installTemplate bool
	installConfig   string
)

var installCmd = &cobra.Command{
	Use:   "install <package[@version]>",
	Short: "패키지 설치 (AI 추천 strategy 자동 선택)",
	Args:  cobra.ExactArgs(1),
	RunE:  runInstall,
}

func init() {
	installCmd.Flags().StringVar(&installStrategy, "strategy", "", "strategy 강제 지정 (native|docker|k8s|helm)")
	installCmd.Flags().BoolVar(&installYes, "yes", false, "비대화형 모드 (확인 스킵)")
	installCmd.Flags().BoolVar(&installNative, "native", false, "--strategy=native 단축키")
	installCmd.Flags().BoolVar(&installDocker, "docker", false, "--strategy=docker 단축키")
	installCmd.Flags().BoolVar(&installK8s, "k8s", false, "--strategy=k8s 단축키")
	installCmd.Flags().BoolVar(&installHelm, "helm", false, "--strategy=helm 단축키")
	installCmd.Flags().BoolVar(&installTemplate, "template", false, "config template을 ./<slug>.env로 저장")
	installCmd.Flags().StringVar(&installConfig, "config", "", "커스텀 config 파일 (MOSO_CONFIG 환경변수로 주입)")
	rootCmd.AddCommand(installCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	slug, version := hub.ParseSlugVersion(args[0])

	preferred := installStrategy
	if installNative {
		preferred = "native"
	} else if installDocker {
		preferred = "docker"
	} else if installK8s {
		preferred = "k8s"
	} else if installHelm {
		preferred = "helm"
	}

	cfg, err := config.DefaultLoad()
	if err != nil {
		return err
	}
	if hubURL != "" {
		cfg.HubURL = hubURL
	}

	// --template: download config template and exit early.
	if installTemplate {
		return runInstallTemplate(slug, preferred, cfg)
	}

	// 1. OS 스펙 수집
	home, _ := os.UserHomeDir()
	morphsoDir := filepath.Join(home, ".morphso")
	s, err := spec.GetOrCollect(morphsoDir)
	if err != nil {
		return fmt.Errorf("OS 스펙 수집 실패: %w", err)
	}

	// 2. 패키지 정보 조회
	client := hub.NewClient(cfg.HubURL, cfg.Token)
	pkg, err := client.GetPackage(slug)
	if errors.Is(err, hub.ErrNotFound) {
		return fmt.Errorf("패키지 '%s'를 찾을 수 없습니다", slug)
	}
	if err != nil {
		return fmt.Errorf("패키지 조회 실패: %w", err)
	}

	// 3. strategy 추천
	var strategy, reason string
	if cfg.Token != "" {
		result, err := client.Recommend(slug, s, preferred)
		if err == nil {
			strategy = result.Strategy
			reason = result.Reason
		}
	}
	if strategy == "" {
		strategy = hub.LocalRecommend(s, preferred)
		reason = "로컬 rule-based 추천 (hub 미연결 또는 미로그인)"
	}

	// 4. 추천 결과 출력 + strategy 확인
	fmt.Printf("\n추천: --%s\n", strategy)
	fmt.Printf("이유: %s\n\n", reason)

	if !installYes {
		fmt.Printf("진행하시겠습니까? [Y/n/native/docker/k8s/helm] ")
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))
		switch input {
		case "", "y", "yes":
		case "n", "no":
			fmt.Println("취소됨.")
			return nil
		case "native", "docker", "k8s", "helm":
			strategy = input
			fmt.Printf("strategy를 '%s'(으)로 변경합니다.\n", strategy)
		default:
			fmt.Println("취소됨.")
			return nil
		}
	}

	// 5. 필요 도구 확인
	if req := prerequisiteForStrategy(strategy, pkg.Type); req != "" && !installer.CheckPrerequisite(req) {
		return fmt.Errorf("'%s'가 설치되어 있지 않습니다. 먼저 설치하세요", req)
	}

	// 6. Hub에서 install script 조회 → 없으면 로컬 BuildCommand fallback
	installScript, err := client.GetInstallScript(slug, version, strategy)
	if err == nil {
		if err := runScriptFlow(installScript, slug, version); err != nil {
			return err
		}
		if cfg.Token != "" {
			_ = client.RecordInstall(slug, version, strategy)
		}
		return nil
	}
	if !errors.Is(err, hub.ErrNotFound) {
		return fmt.Errorf("install script 조회 실패: %w", err)
	}

	// Fallback: local BuildCommand (hub에 스크립트 미등록 패키지)
	command := installer.BuildCommand(pkg.Type, strategy, slug, version)
	fmt.Printf("\n실행: %s\n\n", strings.Join(command, " "))
	if err := installer.Run(command, os.Stdout); err != nil {
		return fmt.Errorf("설치 실패: %w", err)
	}
	if cfg.Token != "" {
		_ = client.RecordInstall(slug, version, strategy)
	}
	fmt.Printf("\n✓ '%s' 설치 완료!\n", slug)
	return nil
}

// runScriptFlow previews, confirms, and executes the hub-provided install script.
func runScriptFlow(script *hub.InstallScript, slug, version string) error {
	// SHA256 무결성 검증
	hash := sha256.Sum256([]byte(script.Script))
	computed := fmt.Sprintf("sha256:%x", hash)
	if computed != script.SHA256 {
		return fmt.Errorf("스크립트 무결성 오류: 예상 %s, 실제 %s", script.SHA256, computed)
	}

	// Preview + confirm
	if !installYes {
		fmt.Println("\n─── install script preview ─────────────────────────")
		fmt.Println(script.Script)
		fmt.Println("────────────────────────────────────────────────────")
		if script.ModelUsed != "" {
			fmt.Printf("(generated by: %s)\n\n", script.ModelUsed)
		}
		fmt.Print("Proceed? [Y/n] ")
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))
		if input == "n" || input == "no" {
			fmt.Println("취소됨.")
			return nil
		}
	}

	// config 파일 존재 여부 검증
	if installConfig != "" {
		if _, err := os.Stat(installConfig); err != nil {
			return fmt.Errorf("config 파일을 찾을 수 없습니다: %s", installConfig)
		}
	}

	// 임시 파일에 스크립트 저장 후 실행
	tmpFile, err := os.CreateTemp("", fmt.Sprintf("moso-install-%s-*.sh", slug))
	if err != nil {
		return fmt.Errorf("임시 파일 생성 실패: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(script.Script); err != nil {
		tmpFile.Close()
		return fmt.Errorf("스크립트 쓰기 실패: %w", err)
	}
	tmpFile.Close()

	if err := os.Chmod(tmpFile.Name(), 0700); err != nil {
		return fmt.Errorf("chmod 실패: %w", err)
	}

	c := exec.Command("sh", tmpFile.Name())
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if installConfig != "" {
		c.Env = append(os.Environ(), "MOSO_CONFIG="+installConfig)
	}
	if err := c.Run(); err != nil {
		return fmt.Errorf("설치 실패: %w", err)
	}

	fmt.Printf("\n✓ '%s@%s' 설치 완료!\n", slug, version)
	return nil
}

// runInstallTemplate downloads the config template and saves it locally.
func runInstallTemplate(slug, strategy string, cfg *config.Config) error {
	if strategy == "" {
		strategy = "docker"
	}
	client := hub.NewClient(cfg.HubURL, cfg.Token)
	tmpl, err := client.GetInstallTemplate(slug, strategy)
	if errors.Is(err, hub.ErrNotFound) {
		return fmt.Errorf("'%s' 패키지에 %s config template이 없습니다", slug, strategy)
	}
	if err != nil {
		return fmt.Errorf("template 조회 실패: %w", err)
	}
	filename := slug + ".env"
	if err := os.WriteFile(filename, []byte(tmpl), 0644); err != nil {
		return fmt.Errorf("template 저장 실패: %w", err)
	}
	fmt.Printf("Config template saved to ./%s\n", filename)
	fmt.Printf("Edit it and re-run:\n  moso install %s --%s --config ./%s\n", slug, strategy, filename)
	return nil
}

func prerequisiteForStrategy(strategy, pkgType string) string {
	switch strategy {
	case "docker":
		return "docker"
	case "k8s", "helm":
		return "helm"
	case "native":
		switch pkgType {
		case "npm":
			return "npm"
		case "brew":
			return "brew"
		}
	}
	return ""
}
