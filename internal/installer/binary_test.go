package installer

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// fakeAK serves the artifact-keeper generic-repo endpoints: a listing under
// ?path=<ns> and per-artifact GETs.
func fakeAK(t *testing.T) *httptest.Server {
	t.Helper()
	const listing = `{"items":[
		{"path":"gopedia/v0.4.7/api-linux-amd64","version":"v0.4.7"},
		{"path":"gopedia/v0.4.7/gopedia-linux-amd64","version":"v0.4.7"},
		{"path":"gopedia/v0.4.10/api-linux-amd64","version":"v0.4.10"},
		{"path":"gopedia/v0.4.10/gopedia-linux-amd64","version":"v0.4.10"},
		{"path":"gopedia/v0.4.10/gopedia-darwin-arm64","version":"v0.4.10"},
		{"path":"gopedia/latest/api-linux-amd64","version":"latest"}
	]}`
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/repositories/neunexus-release/artifacts", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(listing))
	})
	mux.HandleFunc("/api/v1/repositories/neunexus-release/download/", func(w http.ResponseWriter, r *http.Request) {
		// echo the artifact path as the "binary" content so we can assert it
		_, _ = w.Write([]byte("ELF:" + r.URL.Path))
	})
	return httptest.NewServer(mux)
}

func newTestInstaller(base string) *BinaryInstaller {
	return &BinaryInstaller{BaseURL: base, Platform: "linux-amd64"}
}

func TestResolveVersion_PicksHighestSemverIgnoringLatest(t *testing.T) {
	srv := fakeAK(t)
	defer srv.Close()
	bi := newTestInstaller(srv.URL)

	v, err := bi.ResolveVersion("gopedia")
	if err != nil {
		t.Fatalf("ResolveVersion: %v", err)
	}
	// v0.4.10 must beat v0.4.7 (numeric, not lexical) and the immutable "latest".
	if v != "v0.4.10" {
		t.Errorf("resolved version = %q, want v0.4.10", v)
	}
}

func TestBinariesFor_ParsesPlatformBinaries(t *testing.T) {
	srv := fakeAK(t)
	defer srv.Close()
	bi := newTestInstaller(srv.URL)

	items, err := bi.listArtifacts("gopedia")
	if err != nil {
		t.Fatalf("listArtifacts: %v", err)
	}
	got := binariesFor(items, "gopedia", "v0.4.10", "linux-amd64")
	sort.Strings(got)
	want := []string{"api", "gopedia"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("binariesFor = %v, want %v", got, want)
	}
	// darwin-arm64 only has gopedia at v0.4.10
	d := binariesFor(items, "gopedia", "v0.4.10", "darwin-arm64")
	if len(d) != 1 || d[0] != "gopedia" {
		t.Errorf("darwin binaries = %v, want [gopedia]", d)
	}
}

func TestInstallBinary_DownloadsAllAndChmods(t *testing.T) {
	srv := fakeAK(t)
	defer srv.Close()
	bi := newTestInstaller(srv.URL)
	dir := t.TempDir()

	installed, err := bi.InstallBinary("gopedia", "", dir, nil) // empty version → resolve latest semver
	if err != nil {
		t.Fatalf("InstallBinary: %v", err)
	}
	sort.Strings(installed)
	if len(installed) != 2 || installed[0] != "api" || installed[1] != "gopedia" {
		t.Fatalf("installed = %v, want [api gopedia]", installed)
	}
	for _, name := range installed {
		p := filepath.Join(dir, name)
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if fi.Mode().Perm()&0o111 == 0 {
			t.Errorf("%s not executable (mode %v)", name, fi.Mode())
		}
		data, _ := os.ReadFile(p)
		// content should be the resolved v0.4.10 artifact, not latest
		if want := "v0.4.10/" + name + "-linux-amd64"; !contains(string(data), want) {
			t.Errorf("%s content = %q, want to contain %q", name, string(data), want)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
