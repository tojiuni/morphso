package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

var (
	ErrAuthorizationPending = errors.New("authorization pending")
	ErrSlowDown             = errors.New("slow down")
	ErrAccessDenied         = errors.New("access denied")
	ErrExpired              = errors.New("device code expired")
)

const defaultClientID = "morphso-cli"

type DeviceFlow struct {
	issuer   string
	clientID string
	http     *http.Client
}

func NewDeviceFlow(issuer, clientID string) *DeviceFlow {
	if clientID == "" {
		clientID = defaultClientID
	}
	return &DeviceFlow{issuer: issuer, clientID: clientID, http: &http.Client{}}
}

type StartResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

func (f *DeviceFlow) Start() (*StartResponse, error) {
	body := url.Values{
		"client_id": {f.clientID},
		"scope":     {"openid profile email"},
	}
	resp, err := f.http.Post(
		f.issuer+"/oauth/v2/device_authorization",
		"application/x-www-form-urlencoded",
		strings.NewReader(body.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("device authorization: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device authorization failed: %s", resp.Status)
	}
	var result StartResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode device response: %w", err)
	}
	return &result, nil
}

// PollOnce makes one token poll attempt.
// Returns (token, nil) on success, ("", ErrAuthorizationPending) while waiting,
// or another error if the flow fails permanently.
func (f *DeviceFlow) PollOnce(deviceCode string) (string, error) {
	body := url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"client_id":   {f.clientID},
		"device_code": {deviceCode},
	}
	resp, err := f.http.Post(
		f.issuer+"/oauth/v2/token",
		"application/x-www-form-urlencoded",
		strings.NewReader(body.Encode()),
	)
	if err != nil {
		return "", fmt.Errorf("token poll: %w", err)
	}
	defer resp.Body.Close()

	var payload struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}

	if payload.AccessToken != "" {
		return payload.AccessToken, nil
	}

	switch payload.Error {
	case "authorization_pending":
		return "", ErrAuthorizationPending
	case "slow_down":
		return "", ErrSlowDown
	case "access_denied":
		return "", ErrAccessDenied
	case "expired_token":
		return "", ErrExpired
	default:
		return "", fmt.Errorf("token error: %s", payload.Error)
	}
}
