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

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/tojiuni/morphso/internal/config"
	"github.com/tojiuni/morphso/internal/hub"
	"github.com/tojiuni/morphso/internal/installer"
	"github.com/tojiuni/morphso/internal/spec"
)

const defaultOllamaURL = "http://localhost:11434"

var (
	installStrategy    string
	installYes         bool
	installNative      bool
	installDocker      bool
	installK8s         bool
	installHelm        bool
	installTemplate    bool
	installConfig      string
	installNoDeps      bool
	installReconfigure bool
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
	installCmd.Flags().BoolVar(&installNoDeps, "no-deps", false, "의존성 설치 없이 main만 설치")
	installCmd.Flags().BoolVar(&installReconfigure, "reconfigure", false, "MCP 패키지 재설치 시 env를 새로 입력")
	rootCmd.AddCommand(installCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	slug, version := hub.ParseSlugVersion(args[0])

	// Single shared stdin reader — multiple bufio.NewReader(os.Stdin) callers
	// each buffer data from the pipe, causing earlier readers to silently consume
	// input meant for later prompts.
	stdinReader := bufio.NewReader(os.Stdin)

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
	if preferred != "" {
		// 사용자가 strategy를 명시(--docker 등)했으면 추천/로그인이 불필요하다.
		// 이 경로는 hub 연결·로그인 없이도 그대로 동작한다.
		strategy = preferred
		reason = "사용자 지정 (--" + preferred + ")"
	} else if cfg.Token != "" {
		result, err := client.Recommend(slug, s, preferred)
		if err == nil {
			strategy = result.Strategy
			reason = result.Reason
		}
	}
	if strategy == "" {
		strategy = hub.RecommendForType(string(pkg.Type), s, preferred)
		reason = "로컬 rule-based 추천 (hub 미연결 또는 미로그인)"
	}

	// 4. 추천 결과 출력 + strategy 확인
	fmt.Printf("\n추천: --%s\n", strategy)
	fmt.Printf("이유: %s\n\n", reason)

	if !installYes {
		fmt.Printf("진행하시겠습니까? [Y/n/native/docker/k8s/helm] ")
		input, _ := stdinReader.ReadString('\n')
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

	// 5.5. Dependency install flow
	groupID, optionalEnv, err := runDepsFlow(client, slug, version, strategy, s, cfg.Token != "", stdinReader)
	if err != nil {
		return err
	}
	if groupID == "" {
		// User cancelled during resource warning prompt
		return nil
	}

	// 5.7. binary + native: download cross-compiled binaries directly from the
	// artifact-keeper generic repo. This is deterministic, so we bypass the hub's
	// (LLM-generated) install script entirely.
	if pkg.Type == "binary" && strategy == "native" {
		binDir := installer.DefaultBinDir()
		bi := &installer.BinaryInstaller{}
		installed, err := bi.InstallBinary(slug, version, binDir, os.Stdout)
		if err != nil {
			return fmt.Errorf("binary 설치 실패: %w", err)
		}
		fmt.Printf("✓ %s → %s\n", strings.Join(installed, ", "), binDir)
		if !installer.OnPath(binDir) {
			fmt.Printf("⚠ %s 가 PATH에 없습니다. 셸 설정에 추가하세요:\n  export PATH=\"%s:$PATH\"\n", binDir, binDir)
		}
		if cfg.Token != "" {
			_, _ = client.RecordInstall(slug, version, strategy, groupID, "user")
		}
		fmt.Printf("\n✓ '%s' 설치 완료!\n", slug)
		return nil
	}

	// 6. Hub에서 install script 조회 → 없으면 로컬 BuildCommand fallback
	installScript, err := client.GetInstallScript(slug, version, strategy)
	if err == nil {
		if err := runScriptFlow(installScript, slug, version, append(specEnv(s), optionalEnv...), stdinReader); err != nil {
			return err
		}
		// MCP client registration (only for type=mcp packages). Failure here
		// does not fail the install — print a manual hint and continue.
		if pkg.Type == "mcp" {
			if regErr := runMCPRegistration(pkg, strategy, version, stdinReader, os.Stdout, installYes, installReconfigure); regErr != nil {
				fmt.Printf("⚠ MCP 클라이언트 자동 등록 실패: %v\n", regErr)
				if pkg.MCPMetadata != nil {
					fmt.Printf("  (수동 등록: 'claude mcp add-json %s ...' 또는 ~/.cursor/mcp.json / ~/.gemini/settings.json 편집)\n", pkg.MCPMetadata.ServerName)
				}
			}
		}
		if cfg.Token != "" {
			_, _ = client.RecordInstall(slug, version, strategy, groupID, "user")
		}
		return nil
	}
	if !errors.Is(err, hub.ErrNotFound) {
		return fmt.Errorf("install script 조회 실패: %w", err)
	}

	// Fallback: local BuildCommand (hub에 스크립트 미등록 패키지)
	if err := errorIfMCPFallbackUnsupported(pkg.Type, strategy); err != nil {
		return err
	}
	command := installer.BuildCommand(pkg.Type, strategy, slug, version)
	fmt.Printf("\n실행: %s\n\n", strings.Join(command, " "))
	if err := installer.Run(command, os.Stdout); err != nil {
		return fmt.Errorf("설치 실패: %w", err)
	}
	// MCP client registration on the fallback path too — same gating/UX as the
	// hub-script path so mcp+native without a hub script still gets registered.
	if pkg.Type == "mcp" {
		if regErr := runMCPRegistration(pkg, strategy, version, stdinReader, os.Stdout, installYes, installReconfigure); regErr != nil {
			fmt.Printf("⚠ MCP 클라이언트 자동 등록 실패: %v\n", regErr)
			if pkg.MCPMetadata != nil {
				fmt.Printf("  (수동 등록: 'claude mcp add-json %s ...' 또는 ~/.cursor/mcp.json / ~/.gemini/settings.json 편집)\n", pkg.MCPMetadata.ServerName)
			}
		}
	}
	if cfg.Token != "" {
		_, _ = client.RecordInstall(slug, version, strategy, groupID, "user")
	}
	fmt.Printf("\n✓ '%s' 설치 완료!\n", slug)
	return nil
}

// newInstallGroupID generates a fresh UUID for grouping related installs.
func newInstallGroupID() string {
	return uuid.New().String()
}

// computeTotals sums memory and disk requirements for all non-skipped plan items
// plus the main package's requirements.
func computeTotals(plan []hub.DepPlanItem, mainDep hub.DependencyInfo) (memGB, diskGB float64) {
	memGB = mainDep.ResourceRequirements.MinMemoryGB
	diskGB = mainDep.ResourceRequirements.MinDiskGB
	for _, item := range plan {
		if item.Action == hub.DepSkip {
			continue
		}
		memGB += item.Dep.ResourceRequirements.MinMemoryGB
		diskGB += item.Dep.ResourceRequirements.MinDiskGB
	}
	return
}

// runDepsFlow fetches dependencies, builds a plan, checks resources,
// shows the plan to the user, confirms, then installs deps sequentially.
// runDepsFlow installs required deps, prompts for optional deps, and returns
// the group ID and any extra env vars collected from optional dep choices.
// Returns ("", nil, nil) to signal user cancellation.
func runDepsFlow(client *hub.Client, slug, version, strategy string, s *spec.Spec, hasToken bool, reader *bufio.Reader) (groupID string, optionalEnv []string, err error) {
	groupID = newInstallGroupID()

	if installNoDeps {
		return groupID, nil, nil
	}

	// Fetch dependencies
	depResp, err := client.GetDependencies(slug)
	if errors.Is(err, hub.ErrNotFound) {
		return groupID, nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	if len(depResp.Dependencies) == 0 {
		return groupID, nil, nil
	}

	// Split required vs optional
	var requiredDeps, optDeps []hub.DependencyInfo
	for _, d := range depResp.Dependencies {
		if d.Optional {
			optDeps = append(optDeps, d)
		} else {
			requiredDeps = append(requiredDeps, d)
		}
	}

	// Fetch install history (best-effort: ignore errors)
	var history []hub.InstallRecord
	if hasToken {
		history, _ = client.GetInstalls()
	}

	// Build plan for required deps only
	plan := hub.BuildDependencyPlan(requiredDeps, history)
	mainDep := hub.DependencyInfo{Package: hub.Package{Slug: slug}}
	warnings := hub.CheckResources(plan, mainDep, s.MemoryFreeGB, s.DiskFreeGB)

	// Print required dep plan
	if len(plan) > 0 {
		fmt.Printf("\nDependencies:\n")
		for _, item := range plan {
			switch item.Action {
			case hub.DepSkip:
				fmt.Printf("  ✓ %-20s %s (설치됨, skip)\n", item.Dep.Package.Slug, item.InstalledVersion)
			case hub.DepUpdate:
				fmt.Printf("  ↑ %-20s %s → %s (업데이트)\n", item.Dep.Package.Slug, item.InstalledVersion, item.Dep.MinVersion)
			case hub.DepInstall:
				fmt.Printf("  + %-20s %s (신규 설치)\n", item.Dep.Package.Slug, item.Dep.MinVersion)
			}
		}
	}

	// Print resource summary
	totalMemNeed, totalDiskNeed := computeTotals(plan, mainDep)
	fmt.Printf("\nRequired:  RAM %.0fGB  Disk %.0fGB\n", totalMemNeed, totalDiskNeed)
	fmt.Printf("Available: RAM %.0fGB  Disk %.0fGB\n", s.MemoryFreeGB, s.DiskFreeGB)

	for _, w := range warnings {
		fmt.Printf("⚠ %s %.0fGB 부족합니다.\n", w.Kind, w.ShortByGB)
	}

	if len(warnings) > 0 && !installYes {
		fmt.Print("계속 진행할까요? [y/N] ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))
		if input != "y" && input != "yes" {
			fmt.Println("취소됨.")
			return "", nil, nil
		}
	}

	// Install required deps
	for _, item := range plan {
		if item.Action == hub.DepSkip {
			continue
		}
		depSlug := item.Dep.Package.Slug
		depVersion := item.Dep.MinVersion

		fmt.Printf("\n[dep] %s 설치 중...\n", depSlug)

		depScript, scriptErr := client.GetInstallScript(depSlug, depVersion, strategy)
		if scriptErr == nil {
			if runErr := runScriptFlow(depScript, depSlug, depVersion, specEnv(s), reader); runErr != nil {
				if errors.Is(runErr, hub.ErrUserCancelled) {
					return "", nil, hub.ErrUserCancelled
				}
				return "", nil, fmt.Errorf("required dep '%s' 설치 실패: %w", depSlug, runErr)
			}
			_, _ = client.RecordInstall(depSlug, depVersion, strategy, groupID, "dependency")
		} else if errors.Is(scriptErr, hub.ErrNotFound) {
			depPkg, fetchErr := client.GetPackage(depSlug)
			if fetchErr != nil {
				return "", nil, fmt.Errorf("required dep '%s' 패키지 정보 조회 실패: %w", depSlug, fetchErr)
			}
			command := installer.BuildCommand(depPkg.Type, strategy, depSlug, depVersion)
			fmt.Printf("실행: %s\n", strings.Join(command, " "))
			if runErr := installer.Run(command, os.Stdout); runErr != nil {
				return "", nil, fmt.Errorf("required dep '%s' 설치 실패: %w", depSlug, runErr)
			}
			_, _ = client.RecordInstall(depSlug, depVersion, strategy, groupID, "dependency")
		} else {
			return "", nil, fmt.Errorf("required dep '%s' script 조회 실패: %w", depSlug, scriptErr)
		}
	}

	// Handle optional deps
	for _, dep := range optDeps {
		env, installErr := promptOptionalDep(client, dep, strategy, groupID, s, reader)
		if installErr != nil {
			fmt.Printf("⚠ optional dep '%s' 처리 실패: %v\n", dep.Package.Slug, installErr)
		}
		optionalEnv = append(optionalEnv, env...)
	}

	return groupID, optionalEnv, nil
}

// promptOptionalDep shows install/url/token choices for one optional dependency.
// Returns env vars to inject into the main package's install script.
func promptOptionalDep(client *hub.Client, dep hub.DependencyInfo, strategy, groupID string, s *spec.Spec, reader *bufio.Reader) ([]string, error) {
	depSlug := dep.Package.Slug
	depName := dep.Package.Name
	if depName == "" {
		depName = depSlug
	}

	fmt.Printf("\n[optional] %s — LLM 설정을 선택하세요:\n", depName)
	fmt.Printf("  [1] %s 신규 설치 (Docker)\n", depName)
	fmt.Printf("  [2] 기존 %s URL 입력\n", depName)
	fmt.Printf("  [3] API 토큰 입력 (OpenAI / Anthropic / 기타)\n")
	fmt.Printf("  [4] 건너뜀\n")

	var input string
	if installYes {
		// 비대화형(--yes): optional dep도 [1] 신규 설치(Docker)로 자동 선택.
		input = "1"
		fmt.Println("선택 [1-4]: 1 (--yes: 신규 설치 자동 선택)")
	} else {
		fmt.Print("선택 [1-4]: ")
		in, _ := reader.ReadString('\n')
		input = strings.TrimSpace(in)
	}

	switch input {
	case "1":
		fmt.Printf("\n[dep] %s 설치 중...\n", depSlug)
		depScript, err := client.GetInstallScript(depSlug, dep.MinVersion, strategy)
		if err != nil {
			return nil, fmt.Errorf("optional dep '%s' 스크립트 조회 실패: %w", depSlug, err)
		}
		if runErr := runScriptFlow(depScript, depSlug, dep.MinVersion, specEnv(s), reader); runErr != nil {
			if errors.Is(runErr, hub.ErrUserCancelled) {
				return nil, nil
			}
			return nil, fmt.Errorf("optional dep '%s' 설치 실패: %w", depSlug, runErr)
		}
		_, _ = client.RecordInstall(depSlug, dep.MinVersion, strategy, groupID, "dependency")
		// MCP optional dep → also register with detected MCP clients.
		// Fetch full package (deps endpoint returns slim metadata; we need MCPMetadata).
		if dep.Package.Type == "mcp" {
			if fullPkg, fetchErr := client.GetPackage(depSlug); fetchErr == nil && fullPkg.MCPMetadata != nil {
				if regErr := runMCPRegistration(fullPkg, strategy, dep.MinVersion, reader, os.Stdout, installYes, installReconfigure); regErr != nil {
					fmt.Printf("⚠ optional dep '%s' MCP 클라이언트 등록 실패: %v\n", depSlug, regErr)
				}
			}
			return nil, nil
		}
		return []string{"OLLAMA_URL=" + defaultOllamaURL}, nil

	case "2":
		fmt.Printf("%s URL 입력 (예: http://localhost:11434): ", depName)
		url, _ := reader.ReadString('\n')
		url = strings.TrimSpace(url)
		if url == "" {
			fmt.Println("URL이 비어있어 건너뜁니다.")
			return nil, nil
		}
		return []string{"OLLAMA_URL=" + url}, nil

	case "3":
		fmt.Print("API Provider (openai/anthropic/기타): ")
		provider, _ := reader.ReadString('\n')
		provider = strings.TrimSpace(provider)
		fmt.Print("API Token: ")
		token, _ := reader.ReadString('\n')
		token = strings.TrimSpace(token)
		if provider == "" || token == "" {
			fmt.Println("입력이 비어있어 건너뜁니다.")
			return nil, nil
		}
		return []string{
			"LLM_PROVIDER=" + provider,
			"LLM_API_KEY=" + token,
		}, nil

	default:
		fmt.Printf("%s 건너뜀.\n", depName)
		return nil, nil
	}
}

// specEnv converts a collected Spec into environment variables for inject into install scripts.
func specEnv(s *spec.Spec) []string {
	return []string{
		"MOSO_OS=" + s.OS,
		"MOSO_ARCH=" + s.Arch,
	}
}

// runScriptFlow previews, confirms, and executes the hub-provided install script.
func runScriptFlow(script *hub.InstallScript, slug, version string, extraEnv []string, reader *bufio.Reader) error {
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
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))
		if input == "n" || input == "no" {
			fmt.Println("취소됨.")
			return hub.ErrUserCancelled
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
	env := append(os.Environ(), extraEnv...)
	if installConfig != "" {
		env = append(env, "MOSO_CONFIG="+installConfig)
	}
	c.Env = env
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
