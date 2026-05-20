package hub

import (
	"errors"
	"time"
)

var ErrNotFound = errors.New("package not found")
var ErrUnauthorized = errors.New("unauthorized")
var ErrUserCancelled = errors.New("user cancelled")
var ErrDeleteConflict = errors.New("delete conflict: package has dependents")

type DeleteConflictResponse struct {
	Message    string   `json:"message"`
	Dependents []string `json:"dependents"`
}

type Package struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Slug        string       `json:"slug"`
	Description string       `json:"description"`
	Type        string       `json:"type"`
	Verified    bool         `json:"verified"`
	PriceCents  int64        `json:"price_cents"`
	Downloads   int64        `json:"downloads"`
	Tags        []string     `json:"tags"`
	MCPMetadata *MCPMetadata `json:"mcp_metadata,omitempty"`
}

// RecommendResponse mirrors morphso-hub's recommend.Result (no json tags → uppercase keys).
type RecommendResponse struct {
	Strategy string `json:"Strategy"`
	Reason   string `json:"Reason"`
}

type InstallRequest struct {
	PackageSlug    string `json:"package_slug"`
	Version        string `json:"version"`
	Strategy       string `json:"strategy"`
	OS             string `json:"os"`
	Arch           string `json:"arch"`
	InstallGroupID string `json:"install_group_id,omitempty"`
	InstallSource  string `json:"install_source,omitempty"`
}

type InstallRecord struct {
	ID             string    `json:"id"`
	UserID         string    `json:"user_id"`
	PackageSlug    string    `json:"package_slug"`
	Version        string    `json:"version"`
	Strategy       string    `json:"strategy"`
	OS             string    `json:"os"`
	Arch           string    `json:"arch"`
	InstalledAt    time.Time `json:"installed_at"`
	InstallGroupID string    `json:"install_group_id,omitempty"`
	InstallSource  string    `json:"install_source,omitempty"`
	Status         string    `json:"status,omitempty"`
}

type InstallScript struct {
	Script      string `json:"script"`
	SHA256      string `json:"sha256"`
	Version     string `json:"version"`
	HasTemplate bool   `json:"has_template"`
	ModelUsed   string `json:"model_used"`
}

type ResourceRequirements struct {
	MinMemoryGB float64 `json:"min_memory_gb"`
	MinDiskGB   float64 `json:"min_disk_gb"`
	NeedsGPU    bool    `json:"needs_gpu"`
}

type DependencyInfo struct {
	Package              Package              `json:"package"`
	MinVersion           string               `json:"min_version"`
	Source               string               `json:"source"`
	Optional             bool                 `json:"optional"`
	ResourceRequirements ResourceRequirements `json:"resource_requirements"`
}

type DependencyResponse struct {
	Dependencies []DependencyInfo `json:"dependencies"`
}

type InstallRecordID struct {
	ID string `json:"id"`
}

type MCPMetadata struct {
	ServerName        string                 `json:"server_name"`
	Transport         string                 `json:"transport,omitempty"`
	Native            *MCPCommandSpec        `json:"native,omitempty"`
	Docker            *MCPCommandSpec        `json:"docker,omitempty"`
	PlatformOverrides map[string]MCPOverride `json:"platform_overrides,omitempty"`
	EnvSchema         []MCPEnvSpec           `json:"env_schema,omitempty"`
}

type MCPCommandSpec struct {
	Command string   `json:"command,omitempty"`
	Image   string   `json:"image,omitempty"`
	Args    []string `json:"args,omitempty"`
}

type MCPOverride struct {
	Native *MCPCommandSpec `json:"native,omitempty"`
	Docker *MCPCommandSpec `json:"docker,omitempty"`
}

type MCPEnvSpec struct {
	Name     string `json:"name"`
	Prompt   string `json:"prompt"`
	Required bool   `json:"required"`
	Default  string `json:"default,omitempty"`
	Secret   bool   `json:"secret,omitempty"`
}
