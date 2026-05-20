package installer

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// artifact-keeper generic repo holding cross-compiled native binaries, uploaded
// by CI at {ns}/{version}/{binary}-{os}-{arch}. Downloads are anonymous.
const (
	akBaseURL = "https://artifacts.toji.homes"
	akRepo    = "neunexus-release"
)

// BinaryInstaller downloads native binaries from the artifact-keeper generic
// repo and installs them into a bin directory. Fields default to production
// values; tests override BaseURL/Platform.
type BinaryInstaller struct {
	BaseURL  string       // default akBaseURL
	Repo     string       // default akRepo
	Platform string       // "<os>-<arch>", default runtime.GOOS-runtime.GOARCH
	HTTP     *http.Client // default http.DefaultClient
}

type akArtifact struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}

func (b *BinaryInstaller) baseURL() string {
	if b.BaseURL != "" {
		return strings.TrimRight(b.BaseURL, "/")
	}
	return akBaseURL
}

func (b *BinaryInstaller) repo() string {
	if b.Repo != "" {
		return b.Repo
	}
	return akRepo
}

func (b *BinaryInstaller) platform() string {
	if b.Platform != "" {
		return b.Platform
	}
	return runtime.GOOS + "-" + runtime.GOARCH
}

func (b *BinaryInstaller) httpClient() *http.Client {
	if b.HTTP != nil {
		return b.HTTP
	}
	return http.DefaultClient
}

// listArtifacts returns every artifact published under namespace ns.
func (b *BinaryInstaller) listArtifacts(ns string) ([]akArtifact, error) {
	u := fmt.Sprintf("%s/api/v1/repositories/%s/artifacts?path=%s", b.baseURL(), b.repo(), url.QueryEscape(ns))
	resp, err := b.httpClient().Get(u)
	if err != nil {
		return nil, fmt.Errorf("artifact 목록 조회: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("artifact 목록 조회: HTTP %d", resp.StatusCode)
	}
	var out struct {
		Items []akArtifact `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("artifact 목록 파싱: %w", err)
	}
	return out.Items, nil
}

// ResolveVersion picks the highest semver version available for ns, ignoring
// the immutable "latest" alias (which CI cannot overwrite after first upload).
func (b *BinaryInstaller) ResolveVersion(ns string) (string, error) {
	items, err := b.listArtifacts(ns)
	if err != nil {
		return "", err
	}
	best := ""
	for _, it := range items {
		v := it.Version
		if v == "" || v == "latest" {
			continue
		}
		if best == "" || compareSemver(v, best) > 0 {
			best = v
		}
	}
	if best == "" {
		return "", fmt.Errorf("'%s'에 발행된 버전이 없습니다", ns)
	}
	return best, nil
}

// binariesFor returns the distinct binary base names available for ns/version
// on platform, parsed from artifact paths "{ns}/{version}/{binary}-{platform}".
func binariesFor(items []akArtifact, ns, version, platform string) []string {
	prefix := ns + "/" + version + "/"
	suffix := "-" + platform
	seen := map[string]bool{}
	var out []string
	for _, it := range items {
		p := it.Path
		if !strings.HasPrefix(p, prefix) || !strings.HasSuffix(p, suffix) {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(p, prefix), suffix)
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// InstallBinary downloads every binary published for ns at version on the
// current platform into dir (chmod 0755) and returns the installed names.
// An empty version resolves to the highest published semver.
func (b *BinaryInstaller) InstallBinary(ns, version, dir string, out io.Writer) ([]string, error) {
	if out == nil {
		out = os.Stdout
	}
	items, err := b.listArtifacts(ns)
	if err != nil {
		return nil, err
	}
	if version == "" || version == "latest" {
		version, err = b.ResolveVersion(ns)
		if err != nil {
			return nil, err
		}
	}
	plat := b.platform()
	names := binariesFor(items, ns, version, plat)
	if len(names) == 0 {
		return nil, fmt.Errorf("'%s' %s: 플랫폼 %s용 바이너리가 없습니다", ns, version, plat)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("설치 디렉토리 생성 실패: %w", err)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(out, "  ↓ %s %s (%s)\n", name, version, plat)
		if err := b.downloadTo(ns, version, name, plat, dir); err != nil {
			return nil, err
		}
	}
	return names, nil
}

func (b *BinaryInstaller) downloadTo(ns, version, name, plat, dir string) error {
	// NOTE: /artifacts/<path> returns the JSON metadata record; the raw file is
	// served by the /download/<path> route.
	artifact := fmt.Sprintf("%s/%s/%s-%s", ns, version, name, plat)
	u := fmt.Sprintf("%s/api/v1/repositories/%s/download/%s", b.baseURL(), b.repo(), artifact)
	resp, err := b.httpClient().Get(u)
	if err != nil {
		return fmt.Errorf("%s 다운로드: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s 다운로드: HTTP %d", name, resp.StatusCode)
	}
	dest := filepath.Join(dir, name)
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return fmt.Errorf("%s 쓰기: %w", dest, err)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return fmt.Errorf("%s 저장: %w", dest, err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	// O_CREATE honors the mode only for new files; ensure +x even if it existed.
	return os.Chmod(dest, 0o755)
}

// compareSemver compares dotted numeric versions (optionally "v"-prefixed).
// Returns >0 if a>b, <0 if a<b, 0 if equal. Non-numeric components compare
// lexically as a fallback.
func compareSemver(a, b string) int {
	as := strings.Split(strings.TrimPrefix(a, "v"), ".")
	bs := strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var ai, bi string
		if i < len(as) {
			ai = as[i]
		}
		if i < len(bs) {
			bi = bs[i]
		}
		an, aerr := strconv.Atoi(ai)
		bn, berr := strconv.Atoi(bi)
		if aerr == nil && berr == nil {
			if an != bn {
				return an - bn
			}
			continue
		}
		if ai != bi {
			return strings.Compare(ai, bi)
		}
	}
	return 0
}

// DefaultBinDir is the per-user install location (no sudo required).
func DefaultBinDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.Getenv("HOME"), ".local", "bin")
	}
	return filepath.Join(home, ".local", "bin")
}

// OnPath reports whether dir is in $PATH.
func OnPath(dir string) bool {
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if p == dir {
			return true
		}
	}
	return false
}
