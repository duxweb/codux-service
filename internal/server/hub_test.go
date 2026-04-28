package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/duxweb/codux-service/internal/store"
)

func TestPairingRejectAndDeviceRevocationFlow(t *testing.T) {
	database, err := store.Open(t.TempDir() + "/codux-service.sqlite3")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	hub := NewHub(database, slog.New(slog.NewTextHandler(io.Discard, nil)), 5*time.Minute)
	server := httptest.NewServer(hub.Routes())
	t.Cleanup(server.Close)

	post(t, server.URL, "/api/hosts/register", map[string]any{
		"hostId": "host-1",
		"name":   "Mac",
		"token":  "host-token",
	})

	pairing := post(t, server.URL, "/api/pairings", map[string]any{
		"hostId": "host-1",
		"token":  "host-token",
	})
	post(t, server.URL, "/api/pairings/claim", map[string]any{
		"code":      pairing["code"],
		"secret":    pairing["secret"],
		"name":      "Phone",
		"publicKey": "",
	})
	post(t, server.URL, "/api/pairings/reject", map[string]any{
		"hostId":    "host-1",
		"token":     "host-token",
		"pairingId": pairing["pairingId"],
	})
	status := post(t, server.URL, "/api/pairings/status", map[string]any{
		"code":   pairing["code"],
		"secret": pairing["secret"],
	})
	if status["status"] != "rejected" {
		t.Fatalf("expected rejected status, got %#v", status)
	}

	pairing = post(t, server.URL, "/api/pairings", map[string]any{
		"hostId": "host-1",
		"token":  "host-token",
	})
	post(t, server.URL, "/api/pairings/claim", map[string]any{
		"code":      pairing["code"],
		"secret":    pairing["secret"],
		"name":      "Phone",
		"publicKey": "",
	})
	confirmed := post(t, server.URL, "/api/pairings/confirm", map[string]any{
		"hostId":    "host-1",
		"token":     "host-token",
		"pairingId": pairing["pairingId"],
	})
	devices := get(t, server.URL+"/api/hosts/host-1/devices?token=host-token")
	if got := len(devices["devices"].([]any)); got != 1 {
		t.Fatalf("expected one active device, got %d: %#v", got, devices)
	}
	device := devices["devices"].([]any)[0].(map[string]any)
	if device["online"] != false {
		t.Fatalf("expected confirmed device to start offline without websocket, got %#v", device)
	}
	post(t, server.URL, "/api/devices/revoke", map[string]any{
		"hostId":   "host-1",
		"token":    "host-token",
		"deviceId": confirmed["deviceId"],
	})
	devices = get(t, server.URL+"/api/hosts/host-1/devices?token=host-token")
	if got := len(devices["devices"].([]any)); got != 0 {
		t.Fatalf("expected no active devices after revoke, got %d: %#v", got, devices)
	}
}

func post(t *testing.T, baseURL string, path string, body map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	response, err := http.Post(baseURL+path, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("post %s: %v", path, err)
	}
	defer response.Body.Close()
	return decodeResponse(t, response)
}

func get(t *testing.T, url string) map[string]any {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer response.Body.Close()
	return decodeResponse(t, response)
}

func decodeResponse(t *testing.T, response *http.Response) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		t.Fatalf("unexpected status %d: %#v", response.StatusCode, payload)
	}
	return payload
}
