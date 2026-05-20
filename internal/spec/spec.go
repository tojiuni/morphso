package spec

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const cacheTTL = 24 * time.Hour

type Spec struct {
	CollectedAt    time.Time         `yaml:"collected_at"`
	OS             string            `yaml:"os"`
	Arch           string            `yaml:"arch"`
	OSVersion      string            `yaml:"os_version"`
	MemoryTotalGB  float64           `yaml:"memory_total_gb"`
	MemoryFreeGB   float64           `yaml:"memory_free_gb"`
	DiskTotalGB    float64           `yaml:"disk_total_gb"`
	DiskFreeGB     float64           `yaml:"disk_free_gb"`
	GPU            *string           `yaml:"gpu"`
	InstalledTools map[string]string `yaml:"installed_tools"`
}

func Collect() (*Spec, error) {
	s := &Spec{
		CollectedAt:    time.Now(),
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		InstalledTools: make(map[string]string),
	}

	s.OSVersion = collectOSVersion()
	s.MemoryTotalGB, s.MemoryFreeGB = collectMemory()
	s.DiskTotalGB, s.DiskFreeGB = collectDisk()

	tools := []string{"docker", "kubectl", "helm", "pip", "pip3", "npm", "brew"}
	for _, tool := range tools {
		if v := toolVersion(tool); v != "" {
			s.InstalledTools[tool] = v
		}
	}
	if _, ok := s.InstalledTools["pip"]; !ok {
		if v, ok := s.InstalledTools["pip3"]; ok {
			s.InstalledTools["pip"] = v
		}
	}
	delete(s.InstalledTools, "pip3")

	return s, nil
}

func collectOSVersion() string {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("sw_vers", "-productVersion").Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	case "linux":
		data, err := os.ReadFile("/etc/os-release")
		if err != nil {
			return ""
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "VERSION_ID=") {
				return strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), `"`)
			}
		}
	}
	return ""
}

func collectMemory() (total, free float64) {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
		if err == nil {
			if b, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64); err == nil {
				total = float64(b) / (1024 * 1024 * 1024)
			}
		}
		out, err = exec.Command("vm_stat").Output()
		if err == nil {
			parsePages := func(line, prefix string) (int64, bool) {
				if !strings.HasPrefix(line, prefix) {
					return 0, false
				}
				parts := strings.Fields(line)
				if len(parts) < 3 {
					return 0, false
				}
				v, err := strconv.ParseInt(strings.TrimRight(parts[len(parts)-1], "."), 10, 64)
				if err != nil {
					return 0, false
				}
				return v, true
			}
			var pageSize int64 = 4096
			var freePages, specPages, inactivePages, purgeablePages int64
			for _, line := range strings.Split(string(out), "\n") {
				// header: "Mach Virtual Memory Statistics: (page size of 16384 bytes)"
				if strings.Contains(line, "page size of") {
					fields := strings.Fields(line)
					for i, f := range fields {
						if f == "of" && i+1 < len(fields) {
							if v, err := strconv.ParseInt(fields[i+1], 10, 64); err == nil {
								pageSize = v
							}
						}
					}
				}
				if v, ok := parsePages(line, "Pages free:"); ok {
					freePages = v
				}
				if v, ok := parsePages(line, "Pages speculative:"); ok {
					specPages = v
				}
				if v, ok := parsePages(line, "Pages inactive:"); ok {
					inactivePages = v
				}
				if v, ok := parsePages(line, "Pages purgeable:"); ok {
					purgeablePages = v
				}
			}
			// available = free + speculative + inactive + purgeable (all reclaimable)
			free = float64((freePages+specPages+inactivePages+purgeablePages)*pageSize) / (1024 * 1024 * 1024)
		}
	case "linux":
		data, err := os.ReadFile("/proc/meminfo")
		if err != nil {
			return
		}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			kb, _ := strconv.ParseInt(fields[1], 10, 64)
			switch {
			case strings.HasPrefix(line, "MemTotal:"):
				total = float64(kb) / (1024 * 1024)
			case strings.HasPrefix(line, "MemAvailable:"):
				free = float64(kb) / (1024 * 1024)
			}
		}
	}
	return
}

func collectDisk() (total, free float64) {
	out, err := exec.Command("df", "-k", "/").Output()
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 4 {
		return
	}
	if t, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
		total = float64(t) / (1024 * 1024)
	}
	if f, err := strconv.ParseInt(fields[3], 10, 64); err == nil {
		free = float64(f) / (1024 * 1024)
	}
	return
}

func toolVersion(tool string) string {
	var out bytes.Buffer
	cmd := exec.Command(tool, "--version")
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return ""
	}
	line := strings.SplitN(strings.TrimSpace(out.String()), "\n", 2)[0]
	fields := strings.Fields(line)
	for _, f := range fields {
		f = strings.TrimRight(f, ",.")
		if len(f) > 0 && (f[0] >= '0' && f[0] <= '9') {
			return f
		}
	}
	return line
}

func cachePath(dir string) string {
	return filepath.Join(dir, "spec.yaml")
}

func SaveCache(dir string, s *Spec) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create spec dir: %w", err)
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal spec: %w", err)
	}
	return os.WriteFile(cachePath(dir), data, 0600)
}

func LoadCache(dir string) (*Spec, error) {
	data, err := os.ReadFile(cachePath(dir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read spec cache: %w", err)
	}
	var s Spec
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse spec cache: %w", err)
	}
	if time.Since(s.CollectedAt) > cacheTTL {
		return nil, nil
	}
	return &s, nil
}

func GetOrCollect(dir string) (*Spec, error) {
	if s, err := LoadCache(dir); err == nil && s != nil {
		return s, nil
	}
	s, err := Collect()
	if err != nil {
		return nil, err
	}
	_ = SaveCache(dir, s)
	return s, nil
}
