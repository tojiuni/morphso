package hub

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"strings"

	"github.com/tojiuni/morphso/internal/spec"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		http: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // internal private CA
			},
		},
	}
}

func (c *Client) newRequest(method, path string, body any) (*http.Request, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, fmt.Errorf("encode body: %w", err)
		}
	}
	req, err := http.NewRequest(method, c.baseURL+path, &buf)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return req, nil
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusNoContent:
		return nil
	}
	if resp.StatusCode >= 400 {
		body := make([]byte, 256)
		n, _ := resp.Body.Read(body)
		return fmt.Errorf("server error: %s: %s", resp.Status, strings.TrimSpace(string(body[:n])))
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

func (c *Client) Search(query string, limit, offset int) ([]Package, error) {
	u := fmt.Sprintf("/packages?q=%s&limit=%d&offset=%d",
		url.QueryEscape(query), limit, offset)
	req, err := c.newRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	var pkgs []Package
	if err := c.do(req, &pkgs); err != nil {
		return nil, err
	}
	if pkgs == nil {
		pkgs = []Package{}
	}
	return pkgs, nil
}

func (c *Client) GetPackage(slug string) (*Package, error) {
	req, err := c.newRequest(http.MethodGet, "/packages/"+slug, nil)
	if err != nil {
		return nil, err
	}
	var pkg Package
	if err := c.do(req, &pkg); err != nil {
		return nil, err
	}
	return &pkg, nil
}

type recommendBody struct {
	PackageSlug       string     `json:"package_slug"`
	Spec              *spec.Spec `json:"spec"`
	PreferredStrategy *string    `json:"preferred_strategy"`
}

func (c *Client) Recommend(slug string, s *spec.Spec, preferred string) (*RecommendResponse, error) {
	body := recommendBody{PackageSlug: slug, Spec: s}
	if preferred != "" {
		body.PreferredStrategy = &preferred
	}
	req, err := c.newRequest(http.MethodPost, "/recommend", body)
	if err != nil {
		return nil, err
	}
	var result RecommendResponse
	if err := c.do(req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RecordInstall records an install and returns the new record ID.
// groupID and source are optional ("" = omit from request body via omitempty).
func (c *Client) RecordInstall(slug, version, strategy, groupID, source string) (string, error) {
	body := InstallRequest{
		PackageSlug:    slug,
		Version:        version,
		Strategy:       strategy,
		OS:             OS(),
		Arch:           Arch(),
		InstallGroupID: groupID,
		InstallSource:  source,
	}
	req, err := c.newRequest(http.MethodPost, "/installs", body)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return "", ErrUnauthorized
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("server error: %s", resp.Status)
	}
	var result InstallRecordID
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", nil // best-effort
	}
	return result.ID, nil
}

func (c *Client) GetInstallScript(slug, version, strategy string) (*InstallScript, error) {
	path := fmt.Sprintf("/packages/%s/install-script?version=%s&strategy=%s",
		url.PathEscape(slug), url.QueryEscape(version), url.QueryEscape(strategy))
	req, err := c.newRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var result InstallScript
	if err := c.do(req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) GetInstallTemplate(slug, strategy string) (string, error) {
	path := fmt.Sprintf("/packages/%s/install-script/template?strategy=%s",
		url.PathEscape(slug), url.QueryEscape(strategy))
	req, err := c.newRequest(http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", ErrNotFound
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("server error: %s", resp.Status)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return "", fmt.Errorf("read template: %w", err)
	}
	return buf.String(), nil
}

func (c *Client) GetInstalls() ([]InstallRecord, error) {
	req, err := c.newRequest(http.MethodGet, "/users/me/installs", nil)
	if err != nil {
		return nil, err
	}
	var records []InstallRecord
	if err := c.do(req, &records); err != nil {
		return nil, err
	}
	if records == nil {
		records = []InstallRecord{}
	}
	return records, nil
}

func (c *Client) DeletePackage(slug string, cascade, force bool) (*DeleteConflictResponse, error) {
	path := "/packages/" + url.PathEscape(slug)
	params := url.Values{}
	if cascade {
		params.Set("cascade", "true")
	}
	if force {
		params.Set("force", "true")
	}
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	req, err := c.newRequest(http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil, nil
	case http.StatusNotFound:
		return nil, ErrNotFound
	case http.StatusUnauthorized:
		return nil, ErrUnauthorized
	case http.StatusConflict:
		var conflict DeleteConflictResponse
		if err := json.NewDecoder(resp.Body).Decode(&conflict); err != nil {
			return nil, ErrDeleteConflict
		}
		return &conflict, ErrDeleteConflict
	default:
		if resp.StatusCode >= 400 {
			body := make([]byte, 256)
			n, _ := resp.Body.Read(body)
			return nil, fmt.Errorf("server error: %s: %s", resp.Status, strings.TrimSpace(string(body[:n])))
		}
		return nil, nil
	}
}

func (c *Client) GetDependencies(slug string) (*DependencyResponse, error) {
	req, err := c.newRequest(http.MethodGet, "/packages/"+slug+"/dependencies", nil)
	if err != nil {
		return nil, err
	}
	var result DependencyResponse
	if err := c.do(req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// LocalRecommend applies rule-based logic locally (same rules as morphso-hub) for offline/unauthenticated use.
func LocalRecommend(s *spec.Spec, preferred string) string {
	if preferred != "" {
		return preferred
	}
	_, hasKubectl := s.InstalledTools["kubectl"]
	_, hasHelm := s.InstalledTools["helm"]
	_, hasDocker := s.InstalledTools["docker"]

	if hasKubectl && hasHelm && s.MemoryFreeGB >= 4 {
		return "k8s"
	}
	if hasDocker && s.MemoryFreeGB >= 2 {
		return "docker"
	}
	return "native"
}

// OS returns GOOS for use in install records.
func OS() string { return runtime.GOOS }

// Arch returns GOARCH for use in install records.
func Arch() string { return runtime.GOARCH }
