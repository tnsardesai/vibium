package browser

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/session", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))

		resp := sessionResponse{
			Value: sessionValue{
				SessionID: "test-session-123",
				Capabilities: map[string]interface{}{
					"webSocketUrl": "ws://localhost:9222/session/test-session-123",
				},
			},
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	reqBody := map[string]interface{}{
		"capabilities": map[string]interface{}{
			"alwaysMatch": map[string]interface{}{
				"webSocketUrl": true,
			},
		},
	}

	sessionID, wsURL, userDataDir, err := postSession(server.URL, reqBody, false)
	require.NoError(t, err)
	assert.Equal(t, "test-session-123", sessionID)
	assert.Equal(t, "ws://localhost:9222/session/test-session-123", wsURL)
	assert.Empty(t, userDataDir)
}

func TestPostSessionVerbose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := sessionResponse{
			Value: sessionValue{
				SessionID: "verbose-session",
				Capabilities: map[string]interface{}{
					"webSocketUrl": "ws://localhost:9222/session/verbose-session",
				},
			},
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	reqBody := map[string]interface{}{"capabilities": map[string]interface{}{}}

	sessionID, wsURL, _, err := postSession(server.URL, reqBody, true)
	require.NoError(t, err)
	assert.Equal(t, "verbose-session", sessionID)
	assert.Equal(t, "ws://localhost:9222/session/verbose-session", wsURL)
}

func TestPostSessionHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	reqBody := map[string]interface{}{"capabilities": map[string]interface{}{}}

	_, _, _, err := postSession(server.URL, reqBody, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 500")
}

func TestPostSessionMissingWebSocketURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := sessionResponse{
			Value: sessionValue{
				SessionID:    "no-ws-session",
				Capabilities: map[string]interface{}{},
			},
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	reqBody := map[string]interface{}{"capabilities": map[string]interface{}{}}

	_, _, _, err := postSession(server.URL, reqBody, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "webSocketUrl not found")
}

func TestCreateSessionRemote(t *testing.T) {
	var receivedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&receivedBody))

		resp := sessionResponse{
			Value: sessionValue{
				SessionID: "remote-session-456",
				Capabilities: map[string]interface{}{
					"webSocketUrl": "ws://localhost:9222/session/remote-session-456",
				},
			},
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	sessionID, wsURL, err := createSessionRemote(server.URL, false)
	require.NoError(t, err)
	assert.Equal(t, "remote-session-456", sessionID)
	assert.Equal(t, "ws://localhost:9222/session/remote-session-456", wsURL)

	// Verify webSocketUrl is requested
	caps := receivedBody["capabilities"].(map[string]interface{})
	alwaysMatch := caps["alwaysMatch"].(map[string]interface{})
	assert.Equal(t, true, alwaysMatch["webSocketUrl"])
}

func TestCloseLocalNoOpWhenNilCmdAndZeroPort(t *testing.T) {
	result := &LaunchResult{
		WebSocketURL:    "ws://example.com",
		SessionID:       "test-session",
		ChromedriverCmd: nil,
		Port:            0,
	}

	err := result.Close()
	require.NoError(t, err)
}

func TestCloseRemoteDeletesSession(t *testing.T) {
	var deletedPath string
	var deleteMethod string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deleteMethod = r.Method
		deletedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	result := &LaunchResult{
		WebSocketURL:    "ws://example.com",
		SessionID:       "remote-sess-789",
		ChromedriverCmd: nil,
		Port:            0,
		ChromedriverURL: server.URL,
	}

	err := result.Close()
	require.NoError(t, err)
	assert.Equal(t, http.MethodDelete, deleteMethod)
	assert.Equal(t, "/session/remote-sess-789", deletedPath)
}

func TestConnectRemoteSetsResultFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := sessionResponse{
			Value: sessionValue{
				SessionID: "remote-session",
				Capabilities: map[string]interface{}{
					"webSocketUrl": "ws://localhost:9222/session/remote-session",
				},
			},
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	result, err := connectRemote(LaunchOptions{
		ChromedriverURL: server.URL,
	})
	require.NoError(t, err)
	assert.Equal(t, "remote-session", result.SessionID)
	assert.Equal(t, "ws://localhost:9222/session/remote-session", result.WebSocketURL)
	assert.Equal(t, server.URL, result.ChromedriverURL)
	assert.Nil(t, result.ChromedriverCmd)
	assert.Equal(t, 0, result.Port)
}
