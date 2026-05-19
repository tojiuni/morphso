package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tojiuni/morphso/internal/auth"
)

func TestDeviceFlow_Start(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/oauth/v2/device_authorization", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "dev_abc",
			"user_code":        "ABCD-1234",
			"verification_uri": "https://auth.example.com/activate",
			"expires_in":       300,
			"interval":         5,
		})
	}))
	defer srv.Close()

	flow := auth.NewDeviceFlow(srv.URL, "morphso-cli")
	resp, err := flow.Start()
	require.NoError(t, err)
	assert.Equal(t, "dev_abc", resp.DeviceCode)
	assert.Equal(t, "ABCD-1234", resp.UserCode)
}

func TestDeviceFlow_PollOnce_Pending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
	}))
	defer srv.Close()

	flow := auth.NewDeviceFlow(srv.URL, "morphso-cli")
	_, err := flow.PollOnce("dev_code")
	assert.ErrorIs(t, err, auth.ErrAuthorizationPending)
}

func TestDeviceFlow_PollOnce_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/oauth/v2/token", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"access_token": "tok_xyz"})
	}))
	defer srv.Close()

	flow := auth.NewDeviceFlow(srv.URL, "morphso-cli")
	token, err := flow.PollOnce("dev_code")
	require.NoError(t, err)
	assert.Equal(t, "tok_xyz", token)
}
