package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/duxweb/codux-service/internal/store"
	"nhooyr.io/websocket"
)

func TestV3TicketStoresArbitraryJSONForShortPairingQR(t *testing.T) {
	database, err := store.Open(t.TempDir() + "/codux-service.sqlite3")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	hub := NewHub(database, slog.New(slog.NewTextHandler(io.Discard, nil)), 5*time.Minute)
	server := httptest.NewServer(hub.Routes())
	t.Cleanup(server.Close)

	payload := map[string]any{
		"protocolVersion": "v3.0",
		"nested":          map[string]any{"value": "kept"},
		"items":           []any{1, "two", true},
	}
	created := post(t, server.URL, "/v3/api/tickets", payload)
	ticket, ok := created["ticket"].(string)
	if !ok || ticket == "" {
		t.Fatalf("expected ticket, got %#v", created)
	}
	got := get(t, server.URL+"/v3/api/tickets/"+url.QueryEscape(ticket))
	if got["protocolVersion"] != "v3.0" {
		t.Fatalf("expected stored payload, got %#v", got)
	}
	if nested := got["nested"].(map[string]any); nested["value"] != "kept" {
		t.Fatalf("expected arbitrary nested payload, got %#v", got)
	}

	hub.mu.Lock()
	hub.tickets[ticket] = ticketEntry{payload: hub.tickets[ticket].payload, expiresAt: time.Now().UTC().Add(-time.Second)}
	hub.mu.Unlock()
	response, err := http.Get(server.URL + "/v3/api/tickets/" + url.QueryEscape(ticket))
	if err != nil {
		t.Fatalf("get expired ticket: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected expired ticket 404, got %d", response.StatusCode)
	}
}

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
		"hostId":    "host-1",
		"name":      "Mac",
		"token":     "host-token",
		"publicKey": "host-public-key",
	})

	pairing := post(t, server.URL, "/api/pairings", map[string]any{
		"hostId": "host-1",
		"token":  "host-token",
	})
	if pairing["hostPublicKey"] != "host-public-key" || pairing["cryptoVersion"] != float64(1) {
		t.Fatalf("expected pairing response to include host public key and crypto version, got %#v", pairing)
	}
	qrData, err := base64.RawURLEncoding.DecodeString(pairing["qrPayload"].(string))
	if err != nil {
		t.Fatalf("decode qr payload: %v", err)
	}
	var qr map[string]any
	if err := json.Unmarshal(qrData, &qr); err != nil {
		t.Fatalf("decode qr json: %v", err)
	}
	if qr["hostPublicKey"] != "host-public-key" || qr["cryptoVersion"] != float64(1) {
		t.Fatalf("expected qr payload to carry host crypto fields, got %#v", qr)
	}
	if qr["protocolVersion"] != "v3.0" {
		t.Fatalf("expected v3 protocol in qr payload, got %#v", qr)
	}
	transports, ok := qr["transports"].([]any)
	if !ok || len(transports) != 2 {
		t.Fatalf("expected qr payload transports, got %#v", qr["transports"])
	}
	relay, _ := transports[0].(map[string]any)
	webrtc, _ := transports[1].(map[string]any)
	if relay["kind"] != "websocketRelay" || relay["url"] != server.URL {
		t.Fatalf("expected websocket relay candidate, got %#v", relay)
	}
	if webrtc["kind"] != "webRtc" || webrtc["url"] != server.URL {
		t.Fatalf("expected webrtc candidate, got %#v", webrtc)
	}
	iceServers, ok := webrtc["iceServers"].([]any)
	if !ok || len(iceServers) != 1 {
		t.Fatalf("expected webrtc ice servers, got %#v", webrtc["iceServers"])
	}
	iceServer, _ := iceServers[0].(map[string]any)
	urls, ok := iceServer["urls"].([]any)
	if !ok || len(urls) != 2 || urls[0] != "stun:stun.miwifi.com:3478" || urls[1] != "stun:stun.l.google.com:19302" {
		t.Fatalf("unexpected ice server urls: %#v", iceServer["urls"])
	}
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
		t.Fatalf("expected pending pairing to be rejected, got %#v", status)
	}

	pairing = post(t, server.URL, "/api/pairings", map[string]any{
		"hostId": "host-1",
		"token":  "host-token",
	})
	post(t, server.URL, "/api/pairings/claim", map[string]any{
		"code":      pairing["code"],
		"secret":    pairing["secret"],
		"name":      "Phone",
		"publicKey": "device-public-key",
	})
	status = post(t, server.URL, "/api/pairings/status", map[string]any{
		"code":   pairing["code"],
		"secret": pairing["secret"],
	})
	if status["status"] != "claimed" || status["pairingId"] != pairing["pairingId"] || status["devicePublicKey"] != "device-public-key" {
		t.Fatalf("expected claimed status with device data, got %#v", status)
	}
	post(t, server.URL, "/api/pairings/reject", map[string]any{
		"hostId":    "host-1",
		"token":     "host-token",
		"pairingId": pairing["pairingId"],
	})
	status = post(t, server.URL, "/api/pairings/status", map[string]any{
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
	status = post(t, server.URL, "/api/pairings/status", map[string]any{
		"code":   pairing["code"],
		"secret": pairing["secret"],
	})
	if status["status"] != "confirmed" || status["deviceId"] != confirmed["deviceId"] || status["token"] != confirmed["token"] {
		t.Fatalf("expected confirmed status to include device credentials, got %#v", status)
	}
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

func TestLegacyRootRoutesRemainAvailable(t *testing.T) {
	database, err := store.Open(t.TempDir() + "/codux-service.sqlite3")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	hub := NewHub(database, slog.New(slog.NewTextHandler(io.Discard, nil)), 5*time.Minute)
	server := httptest.NewServer(hub.Routes())
	t.Cleanup(server.Close)

	post(t, server.URL, "/api/hosts/register", map[string]any{
		"hostId": "host-legacy",
		"name":   "Mac",
		"token":  "host-token",
	})
	pairing := post(t, server.URL, "/api/pairings", map[string]any{
		"hostId": "host-legacy",
		"token":  "host-token",
	})
	qr := decodeQRPayload(t, pairing["qrPayload"].(string))
	transports, ok := qr["transports"].([]any)
	if !ok || len(transports) == 0 {
		t.Fatalf("expected qr payload transports, got %#v", qr["transports"])
	}
	relay, _ := transports[0].(map[string]any)
	if relay["url"] != server.URL {
		t.Fatalf("expected legacy route to advertise root relay url, got %#v", relay)
	}
}

func TestV3RoutesUseStatelessRelay(t *testing.T) {
	database, err := store.Open(t.TempDir() + "/codux-service.sqlite3")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	hub := NewHub(database, slog.New(slog.NewTextHandler(io.Discard, nil)), 5*time.Minute)
	server := httptest.NewServer(hub.Routes())
	t.Cleanup(server.Close)

	response, err := http.Post(server.URL+"/v3/api/hosts/register", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("post v3 api: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected v3 api to stay disabled, got %d", response.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hostConn, _, err := websocket.Dial(ctx, websocketURL(t, server.URL, "/v3/ws/host?hostId=host-v3"), nil)
	if err != nil {
		t.Fatalf("dial v3 host websocket: %v", err)
	}
	t.Cleanup(func() { hostConn.Close(websocket.StatusNormalClosure, "test done") })
	clientConn, _, err := websocket.Dial(ctx, websocketURL(t, server.URL, "/v3/ws/client?hostId=host-v3&deviceId=device-v3"), nil)
	if err != nil {
		t.Fatalf("dial v3 client websocket: %v", err)
	}
	t.Cleanup(func() { clientConn.Close(websocket.StatusNormalClosure, "test done") })

	readEnvelope(t, hostConn)
	readEnvelope(t, clientConn)
	readEnvelope(t, hostConn)
	writeEnvelope(t, clientConn, envelope{Type: "pairing.request", Payload: mustJSON(map[string]any{"pairingId": "pair-1"})})
	message := readEnvelope(t, hostConn)
	if message.Type != "pairing.request" || message.HostID != "host-v3" || message.DeviceID != "device-v3" {
		t.Fatalf("expected stateless pairing request relay, got %#v", message)
	}
	writeEnvelope(t, hostConn, envelope{Type: "pairing.confirmed", DeviceID: "device-v3", Payload: mustJSON(map[string]any{"deviceId": "device-v3"})})
	message = readEnvelope(t, clientConn)
	if message.Type != "pairing.confirmed" || message.DeviceID != "device-v3" {
		t.Fatalf("expected stateless pairing confirmation relay, got %#v", message)
	}
}

func TestDeviceListReturnsWhileHostAndClientAreConnected(t *testing.T) {
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
		"code":   pairing["code"],
		"secret": pairing["secret"],
		"name":   "Phone",
	})
	confirmed := post(t, server.URL, "/api/pairings/confirm", map[string]any{
		"hostId":    "host-1",
		"token":     "host-token",
		"pairingId": pairing["pairingId"],
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hostConn, _, err := websocket.Dial(ctx, websocketURL(t, server.URL, "/ws/host?hostId=host-1&token=host-token"), nil)
	if err != nil {
		t.Fatalf("dial host websocket: %v", err)
	}
	t.Cleanup(func() { hostConn.Close(websocket.StatusNormalClosure, "test done") })
	clientConn, _, err := websocket.Dial(
		ctx,
		websocketURL(t, server.URL, "/ws/client?deviceId="+url.QueryEscape(confirmed["deviceId"].(string))+"&token="+url.QueryEscape(confirmed["token"].(string))),
		nil,
	)
	if err != nil {
		t.Fatalf("dial client websocket: %v", err)
	}
	t.Cleanup(func() { clientConn.Close(websocket.StatusNormalClosure, "test done") })

	devices := getWithTimeout(t, server.URL+"/api/hosts/host-1/devices?token=host-token", 500*time.Millisecond)
	if got := len(devices["devices"].([]any)); got != 1 {
		t.Fatalf("expected one active device, got %d: %#v", got, devices)
	}
	device := devices["devices"].([]any)[0].(map[string]any)
	if device["online"] != true {
		t.Fatalf("expected connected device to be online, got %#v", device)
	}
}

func TestRelayBlocksUploadMessages(t *testing.T) {
	server, hostConn, clientConn := connectedRelayPair(t)
	_ = server
	readEnvelope(t, hostConn)
	readEnvelope(t, clientConn)
	readEnvelope(t, hostConn)

	writeEnvelope(t, clientConn, envelope{Type: "terminal.upload.start", Payload: mustJSON(map[string]any{"name": "large.bin"})})
	errorMessage := readEnvelope(t, clientConn)
	if errorMessage.Type != "relay.error" || errorMessage.Error != "upload_requires_p2p" {
		t.Fatalf("expected upload relay error, got %#v", errorMessage)
	}
	ensureNoEnvelope(t, hostConn, 100*time.Millisecond)
}

func TestRelayBlocksOversizedMessages(t *testing.T) {
	server, hostConn, clientConn := connectedRelayPair(t)
	_ = server
	readEnvelope(t, hostConn)
	readEnvelope(t, clientConn)
	readEnvelope(t, hostConn)

	payload := bytes.Repeat([]byte("x"), int(websocketRelayMaxMessageBytes)+1)
	writeEnvelope(t, clientConn, envelope{Type: "secure.message", Payload: jsonRawMessage(strconv.Quote(string(payload)))})
	errorMessage := readEnvelope(t, clientConn)
	if errorMessage.Type != "relay.error" || errorMessage.Error != "message_too_large" {
		t.Fatalf("expected oversized relay error, got %#v", errorMessage)
	}
	ensureNoEnvelope(t, hostConn, 100*time.Millisecond)
}

func TestRelayRateLimitsMessages(t *testing.T) {
	server, hostConn, clientConn := connectedRelayPair(t)
	_ = server
	readEnvelope(t, hostConn)
	readEnvelope(t, clientConn)
	readEnvelope(t, hostConn)

	for i := 0; i < websocketRelayBurstLimit+1; i++ {
		writeEnvelope(t, clientConn, envelope{Type: "terminal.input", ID: strconv.Itoa(i), Payload: mustJSON(map[string]any{"data": "x"})})
	}
	for i := 0; i < websocketRelayBurstLimit; i++ {
		message := readEnvelope(t, hostConn)
		if message.Type != "terminal.input" {
			t.Fatalf("expected forwarded input, got %#v", message)
		}
	}
	errorMessage := readEnvelope(t, clientConn)
	if errorMessage.Type != "relay.error" || errorMessage.Error != "rate_limited" {
		t.Fatalf("expected rate limit relay error, got %#v", errorMessage)
	}
}

func TestStatsRecorderWritesRelayEvents(t *testing.T) {
	database, err := store.Open(t.TempDir() + "/codux-service.sqlite3")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	statsPath := filepath.Join(t.TempDir(), "stats.jsonl")
	stats, err := NewStatsRecorder(statsPath, time.Hour, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("open stats recorder: %v", err)
	}

	hub := NewHub(database, slog.New(slog.NewTextHandler(io.Discard, nil)), 5*time.Minute)
	hub.SetStatsRecorder(stats)
	server := httptest.NewServer(hub.Routes())
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hostConn, _, err := websocket.Dial(ctx, websocketURL(t, server.URL, "/v3/ws/host?hostId=host-stats"), nil)
	if err != nil {
		t.Fatalf("dial v3 host websocket: %v", err)
	}
	clientConn, _, err := websocket.Dial(ctx, websocketURL(t, server.URL, "/v3/ws/client?hostId=host-stats&deviceId=device-stats"), nil)
	if err != nil {
		t.Fatalf("dial v3 client websocket: %v", err)
	}

	readEnvelope(t, hostConn)
	readEnvelope(t, clientConn)
	readEnvelope(t, hostConn)
	writeEnvelope(t, clientConn, envelope{Type: "terminal.upload.start", Payload: mustJSON(map[string]any{"name": "large.bin"})})
	readEnvelope(t, clientConn)
	clientConn.Close(websocket.StatusNormalClosure, "test done")
	hostConn.Close(websocket.StatusNormalClosure, "test done")
	if err := stats.Close(); err != nil {
		t.Fatalf("close stats recorder: %v", err)
	}

	data, err := os.ReadFile(statsPath)
	if err != nil {
		t.Fatalf("read stats log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 4 {
		t.Fatalf("expected relay stats events, got %q", string(data))
	}
	if !strings.Contains(string(data), `"event":"connect"`) {
		t.Fatalf("expected connect event, got %q", string(data))
	}
	if !strings.Contains(string(data), `"event":"drop"`) || !strings.Contains(string(data), `"reason":"upload_requires_p2p"`) {
		t.Fatalf("expected upload drop event, got %q", string(data))
	}
	if !strings.Contains(string(data), `"event":"snapshot"`) || !strings.Contains(string(data), `"uploadBlockedTotal":1`) {
		t.Fatalf("expected snapshot counters, got %q", string(data))
	}
}

func connectedRelayPair(t *testing.T) (*httptest.Server, *websocket.Conn, *websocket.Conn) {
	t.Helper()
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
		"code":   pairing["code"],
		"secret": pairing["secret"],
		"name":   "Phone",
	})
	confirmed := post(t, server.URL, "/api/pairings/confirm", map[string]any{
		"hostId":    "host-1",
		"token":     "host-token",
		"pairingId": pairing["pairingId"],
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hostConn, _, err := websocket.Dial(ctx, websocketURL(t, server.URL, "/ws/host?hostId=host-1&token=host-token"), nil)
	if err != nil {
		t.Fatalf("dial host websocket: %v", err)
	}
	t.Cleanup(func() { hostConn.Close(websocket.StatusNormalClosure, "test done") })
	clientConn, _, err := websocket.Dial(
		ctx,
		websocketURL(t, server.URL, "/ws/client?deviceId="+url.QueryEscape(confirmed["deviceId"].(string))+"&token="+url.QueryEscape(confirmed["token"].(string))),
		nil,
	)
	if err != nil {
		t.Fatalf("dial client websocket: %v", err)
	}
	t.Cleanup(func() { clientConn.Close(websocket.StatusNormalClosure, "test done") })
	return server, hostConn, clientConn
}

func writeEnvelope(t *testing.T, conn *websocket.Conn, message envelope) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := wsjsonWrite(ctx, conn, message); err != nil {
		t.Fatalf("write envelope: %v", err)
	}
}

func readEnvelope(t *testing.T, conn *websocket.Conn) envelope {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var message envelope
	if err := wsjsonRead(ctx, conn, &message); err != nil {
		t.Fatalf("read envelope: %v", err)
	}
	return message
}

func ensureNoEnvelope(t *testing.T, conn *websocket.Conn, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var message envelope
	if err := wsjsonRead(ctx, conn, &message); err == nil {
		t.Fatalf("expected no envelope, got %#v", message)
	}
}

func decodeQRPayload(t *testing.T, payload string) map[string]any {
	t.Helper()
	qrData, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("decode qr payload: %v", err)
	}
	var qr map[string]any
	if err := json.Unmarshal(qrData, &qr); err != nil {
		t.Fatalf("decode qr json: %v", err)
	}
	return qr
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

func getWithTimeout(t *testing.T, rawURL string, timeout time.Duration) map[string]any {
	t.Helper()
	client := http.Client{Timeout: timeout}
	response, err := client.Get(rawURL)
	if err != nil {
		t.Fatalf("get %s: %v", rawURL, err)
	}
	defer response.Body.Close()
	return decodeResponse(t, response)
}

func websocketURL(t *testing.T, baseURL string, path string) string {
	t.Helper()
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatalf("parse test server url: %v", err)
	}
	pathURL, err := url.Parse(path)
	if err != nil {
		t.Fatalf("parse websocket path: %v", err)
	}
	parsed.Scheme = "ws"
	parsed.Path = pathURL.Path
	parsed.RawQuery = pathURL.RawQuery
	return parsed.String()
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
