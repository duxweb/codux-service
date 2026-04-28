package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/duxweb/codux-service/internal/crypto"
	"github.com/duxweb/codux-service/internal/store"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

const (
	websocketReadLimitBytes int64 = 32 << 20
	websocketPingInterval         = 25 * time.Second
	websocketPongTimeout          = 10 * time.Second
)

type Hub struct {
	store      *store.DB
	logger     *slog.Logger
	pairingTTL time.Duration
	mu         sync.RWMutex
	hosts      map[string]*peer
	clients    map[string]*peer
}

type peer struct {
	id       string
	hostID   string
	deviceID string
	role     string
	conn     *websocket.Conn
	send     chan envelope
	closed   chan struct{}
}

func NewHub(database *store.DB, logger *slog.Logger, pairingTTL time.Duration) *Hub {
	return &Hub{store: database, logger: logger, pairingTTL: pairingTTL, hosts: map[string]*peer{}, clients: map[string]*peer{}}
}

func (h *Hub) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(cors)
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, response{"ok": true}) })
	r.Route("/api", func(r chi.Router) {
		r.Post("/hosts/register", h.registerHost)
		r.Post("/pairings", h.createPairing)
		r.Post("/pairings/claim", h.claimPairing)
		r.Post("/pairings/status", h.pairingStatus)
		r.Post("/pairings/confirm", h.confirmPairing)
		r.Post("/pairings/reject", h.rejectPairing)
		r.Get("/hosts/{hostID}/devices", h.listDevices)
		r.Post("/devices/revoke", h.revokeDevice)
	})
	r.Get("/ws/host", h.hostSocket)
	r.Get("/ws/client", h.clientSocket)
	return r
}

func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, peer := range h.hosts {
		peer.conn.Close(websocket.StatusGoingAway, "server shutting down")
	}
	for _, peer := range h.clients {
		peer.conn.Close(websocket.StatusGoingAway, "server shutting down")
	}
}

func (h *Hub) registerHost(w http.ResponseWriter, r *http.Request) {
	var req registerHostRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	now := time.Now().UTC()
	if req.HostID == "" {
		req.HostID = uuid.NewString()
	}
	if req.Name == "" {
		req.Name = "Codux Mac"
	}
	if req.Token == "" {
		token, err := crypto.Token(32)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		req.Token = token
	}
	if err := h.store.UpsertHost(r.Context(), store.Host{ID: req.HostID, Name: req.Name, Token: req.Token, PublicKey: req.PublicKey, CreatedAt: now, LastSeen: now}); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, registerHostResponse{HostID: req.HostID, Token: req.Token})
}

func (h *Hub) createPairing(w http.ResponseWriter, r *http.Request) {
	var req createPairingRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if _, ok := h.authenticateHost(r.Context(), req.HostID, req.Token); !ok {
		writeErrorMessage(w, http.StatusUnauthorized, "invalid host token")
		return
	}
	secret, err := crypto.Token(24)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	code := strings.ToUpper(strings.ReplaceAll(uuid.NewString()[:8], "-", ""))
	now := time.Now().UTC()
	pairing := store.Pairing{ID: uuid.NewString(), HostID: req.HostID, Code: code, Secret: secret, Status: "pending", CreatedAt: now, ExpiresAt: now.Add(h.pairingTTL)}
	if err := h.store.CreatePairing(r.Context(), pairing); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	host, _ := h.store.HostByID(r.Context(), req.HostID)
	payloadBytes, _ := json.Marshal(response{"server": publicBaseURL(r), "code": code, "secret": secret})
	qrPayload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	writeJSON(w, http.StatusOK, createPairingResponse{PairingID: pairing.ID, Code: code, Secret: secret, HostName: host.Name, ExpiresAt: pairing.ExpiresAt, QRPayload: qrPayload})
}

func (h *Hub) claimPairing(w http.ResponseWriter, r *http.Request) {
	var req claimPairingRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	pairing, err := h.store.PairingByCode(r.Context(), strings.ToUpper(req.Code))
	if err != nil || pairing.Secret != req.Secret || pairing.ExpiresAt.Before(time.Now()) {
		writeErrorMessage(w, http.StatusNotFound, "pairing not found or expired")
		return
	}
	if req.Name == "" {
		req.Name = "Mobile Device"
	}
	if err := h.store.ClaimPairing(r.Context(), pairing.ID, req.Name, req.PublicKey, time.Now().UTC()); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	h.sendToHost(pairing.HostID, envelope{Type: "pairing.request", HostID: pairing.HostID, Payload: mustJSON(response{"pairingId": pairing.ID, "code": pairing.Code, "deviceName": req.Name, "devicePublicKey": req.PublicKey}), At: time.Now().UnixMilli()})
	writeJSON(w, http.StatusOK, claimPairingResponse{PairingID: pairing.ID, HostID: pairing.HostID, Status: "claimed"})
}

func (h *Hub) pairingStatus(w http.ResponseWriter, r *http.Request) {
	var req pairingStatusRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	pairing, err := h.store.PairingByCode(r.Context(), strings.ToUpper(req.Code))
	if err != nil || pairing.Secret != req.Secret || pairing.ExpiresAt.Before(time.Now()) {
		writeErrorMessage(w, http.StatusNotFound, "pairing not found or expired")
		return
	}
	host, _ := h.store.HostByID(r.Context(), pairing.HostID)
	res := pairingStatusResponse{Status: pairing.Status, HostID: pairing.HostID, HostName: host.Name}
	if pairing.DeviceID != nil && pairing.Status == "confirmed" {
		device, err := h.store.DeviceByID(r.Context(), *pairing.DeviceID)
		if err == nil && device.RevokedAt == nil {
			res.DeviceID = device.ID
			res.Token = device.Token
		}
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *Hub) confirmPairing(w http.ResponseWriter, r *http.Request) {
	var req confirmPairingRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if _, ok := h.authenticateHost(r.Context(), req.HostID, req.Token); !ok {
		writeErrorMessage(w, http.StatusUnauthorized, "invalid host token")
		return
	}
	pairing, err := h.store.PairingByID(r.Context(), req.PairingID)
	if err != nil || pairing.HostID != req.HostID {
		writeErrorMessage(w, http.StatusNotFound, "pairing not found")
		return
	}
	if pairing.Status != "claimed" {
		writeErrorMessage(w, http.StatusConflict, "pairing is not claimed")
		return
	}
	deviceToken, err := crypto.Token(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	now := time.Now().UTC()
	device := store.Device{ID: uuid.NewString(), HostID: req.HostID, Name: pairing.DeviceName, Token: deviceToken, PublicKey: pairing.DevicePublicKey, CreatedAt: now, LastSeen: now}
	if err := h.store.ConfirmPairing(r.Context(), pairing.ID, device, now); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	h.sendToHost(req.HostID, envelope{Type: "pairing.confirmed", HostID: req.HostID, DeviceID: device.ID, Payload: mustJSON(response{"deviceId": device.ID, "deviceName": device.Name}), At: now.UnixMilli()})
	writeJSON(w, http.StatusOK, confirmPairingResponse{DeviceID: device.ID, HostID: req.HostID, Token: deviceToken})
}

func (h *Hub) rejectPairing(w http.ResponseWriter, r *http.Request) {
	var req rejectPairingRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if _, ok := h.authenticateHost(r.Context(), req.HostID, req.Token); !ok {
		writeErrorMessage(w, http.StatusUnauthorized, "invalid host token")
		return
	}
	pairing, err := h.store.PairingByID(r.Context(), req.PairingID)
	if err != nil || pairing.HostID != req.HostID {
		writeErrorMessage(w, http.StatusNotFound, "pairing not found")
		return
	}
	if pairing.Status != "claimed" {
		writeErrorMessage(w, http.StatusConflict, "pairing is not claimed")
		return
	}
	now := time.Now().UTC()
	if err := h.store.RejectPairing(r.Context(), req.HostID, req.PairingID, now); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	h.sendToHost(req.HostID, envelope{Type: "pairing.rejected", HostID: req.HostID, Payload: mustJSON(response{"pairingId": req.PairingID, "deviceName": pairing.DeviceName}), At: now.UnixMilli()})
	writeJSON(w, http.StatusOK, response{"ok": true})
}

func (h *Hub) listDevices(w http.ResponseWriter, r *http.Request) {
	hostID := chi.URLParam(r, "hostID")
	token := bearerOrQuery(r)
	if _, ok := h.authenticateHost(r.Context(), hostID, token); !ok {
		writeErrorMessage(w, http.StatusUnauthorized, "invalid host token")
		return
	}
	devices, err := h.store.DevicesForHost(r.Context(), hostID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	for index := range devices {
		devices[index].Online = h.isClientOnline(devices[index].ID)
	}
	writeJSON(w, http.StatusOK, response{"devices": devices})
}

func (h *Hub) revokeDevice(w http.ResponseWriter, r *http.Request) {
	var req revokeDeviceRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if _, ok := h.authenticateHost(r.Context(), req.HostID, req.Token); !ok {
		writeErrorMessage(w, http.StatusUnauthorized, "invalid host token")
		return
	}
	if err := h.store.RevokeDevice(r.Context(), req.HostID, req.DeviceID, time.Now().UTC()); err != nil && !errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	h.disconnectClient(req.DeviceID, "device revoked")
	writeJSON(w, http.StatusOK, response{"ok": true})
}

func (h *Hub) hostSocket(w http.ResponseWriter, r *http.Request) {
	hostID := r.URL.Query().Get("hostId")
	token := r.URL.Query().Get("token")
	if _, ok := h.authenticateHost(r.Context(), hostID, token); !ok {
		http.Error(w, "invalid host token", http.StatusUnauthorized)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	conn.SetReadLimit(websocketReadLimitBytes)
	peer := newPeer(hostID, hostID, "", "host", conn)
	h.registerPeer(peer)
	h.store.TouchHost(context.Background(), hostID, time.Now().UTC())
	h.runPeer(r.Context(), peer)
}

func (h *Hub) clientSocket(w http.ResponseWriter, r *http.Request) {
	device, ok := h.authenticateDevice(r.Context(), r.URL.Query().Get("deviceId"), r.URL.Query().Get("token"))
	if !ok {
		http.Error(w, "invalid device token", http.StatusUnauthorized)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	conn.SetReadLimit(websocketReadLimitBytes)
	peer := newPeer(device.ID, device.HostID, device.ID, "client", conn)
	h.registerPeer(peer)
	h.store.TouchDevice(context.Background(), device.ID, time.Now().UTC())
	h.runPeer(r.Context(), peer)
}

func (h *Hub) runPeer(ctx context.Context, p *peer) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go h.writeLoop(ctx, p)
	go h.pingLoop(ctx, p)
	p.send <- envelope{Type: "hello", HostID: p.hostID, DeviceID: p.deviceID, Payload: mustJSON(response{"role": p.role}), At: time.Now().UnixMilli()}
	for {
		var msg envelope
		if err := wsjsonRead(ctx, p.conn, &msg); err != nil {
			h.logger.Info("peer read failed", "role", p.role, "host", p.hostID, "device", p.deviceID, "error", err)
			break
		}
		msg.At = time.Now().UnixMilli()
		if p.role == "host" {
			if msg.DeviceID != "" {
				h.sendToClient(msg.DeviceID, msg)
			} else {
				h.broadcastToHostClients(p.hostID, msg)
			}
		} else {
			msg.HostID = p.hostID
			msg.DeviceID = p.deviceID
			if msg.Type == "device.info" {
				var payload struct {
					Name string `json:"name"`
				}
				if len(msg.Payload) > 0 && json.Unmarshal(msg.Payload, &payload) == nil && strings.TrimSpace(payload.Name) != "" {
					_ = h.store.UpdateDeviceName(context.Background(), p.deviceID, strings.TrimSpace(payload.Name), time.Now().UTC())
				}
			}
			h.sendToHost(p.hostID, msg)
		}
	}
	h.unregisterPeer(p)
	p.conn.Close(websocket.StatusNormalClosure, "closed")
}

func (h *Hub) writeLoop(ctx context.Context, p *peer) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-p.send:
			writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			_ = wsjsonWrite(writeCtx, p.conn, msg)
			cancel()
		}
	}
}

func (h *Hub) pingLoop(ctx context.Context, p *peer) {
	ticker := time.NewTicker(websocketPingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, websocketPongTimeout)
			err := p.conn.Ping(pingCtx)
			cancel()
			if err != nil {
				h.logger.Info("peer ping failed", "role", p.role, "host", p.hostID, "device", p.deviceID, "error", err)
				p.conn.Close(websocket.StatusPolicyViolation, "ping timeout")
				return
			}
		}
	}
}

func (h *Hub) registerPeer(p *peer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if p.role == "host" {
		if old := h.hosts[p.hostID]; old != nil {
			old.conn.Close(websocket.StatusPolicyViolation, "replaced")
		}
		h.hosts[p.hostID] = p
	} else {
		if old := h.clients[p.deviceID]; old != nil {
			old.conn.Close(websocket.StatusPolicyViolation, "replaced")
		}
		h.clients[p.deviceID] = p
	}
	h.logger.Info("peer connected", "role", p.role, "host", p.hostID, "device", p.deviceID)
	if p.role == "client" {
		h.sendToHost(p.hostID, envelope{Type: "device.connected", HostID: p.hostID, DeviceID: p.deviceID, Payload: mustJSON(response{"deviceId": p.deviceID}), At: time.Now().UnixMilli()})
	}
}

func (h *Hub) unregisterPeer(p *peer) {
	h.mu.Lock()
	removed := false
	if p.role == "host" && h.hosts[p.hostID] == p {
		delete(h.hosts, p.hostID)
		removed = true
	}
	if p.role == "client" && h.clients[p.deviceID] == p {
		delete(h.clients, p.deviceID)
		removed = true
	}
	h.mu.Unlock()
	if removed {
		h.logger.Info("peer disconnected", "role", p.role, "host", p.hostID, "device", p.deviceID)
		if p.role == "client" {
			h.sendToHost(p.hostID, envelope{Type: "device.disconnected", HostID: p.hostID, DeviceID: p.deviceID, Payload: mustJSON(response{"deviceId": p.deviceID}), At: time.Now().UnixMilli()})
		}
	}
}

func (h *Hub) broadcastToHostClients(hostID string, msg envelope) int {
	h.mu.RLock()
	peers := make([]*peer, 0)
	for _, client := range h.clients {
		if client.hostID == hostID {
			peers = append(peers, client)
		}
	}
	h.mu.RUnlock()
	sent := 0
	for _, client := range peers {
		if sendPeer(client, msg) {
			sent++
		}
	}
	return sent
}

func (h *Hub) sendToHost(hostID string, msg envelope) bool {
	h.mu.RLock()
	p := h.hosts[hostID]
	h.mu.RUnlock()
	return sendPeer(p, msg)
}
func (h *Hub) sendToClient(deviceID string, msg envelope) bool {
	h.mu.RLock()
	p := h.clients[deviceID]
	h.mu.RUnlock()
	return sendPeer(p, msg)
}
func (h *Hub) isClientOnline(deviceID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.clients[deviceID] != nil
}
func sendPeer(p *peer, msg envelope) bool {
	if p == nil {
		return false
	}
	select {
	case p.send <- msg:
		return true
	default:
		return false
	}
}
func (h *Hub) disconnectClient(deviceID, reason string) {
	h.mu.RLock()
	p := h.clients[deviceID]
	h.mu.RUnlock()
	if p != nil {
		p.conn.Close(websocket.StatusPolicyViolation, reason)
	}
}

func newPeer(id, hostID, deviceID, role string, conn *websocket.Conn) *peer {
	return &peer{id: id, hostID: hostID, deviceID: deviceID, role: role, conn: conn, send: make(chan envelope, 128), closed: make(chan struct{})}
}

func (h *Hub) authenticateHost(ctx context.Context, hostID, token string) (store.Host, bool) {
	if hostID == "" || token == "" {
		return store.Host{}, false
	}
	host, err := h.store.HostByToken(ctx, token)
	return host, err == nil && host.ID == hostID
}
func (h *Hub) authenticateDevice(ctx context.Context, deviceID, token string) (store.Device, bool) {
	if deviceID == "" || token == "" {
		return store.Device{}, false
	}
	device, err := h.store.DeviceByToken(ctx, token)
	return device, err == nil && device.ID == deviceID && device.RevokedAt == nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, err error) {
	writeErrorMessage(w, status, err.Error())
}
func writeErrorMessage(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, response{"error": message})
}
func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return false
	}
	return true
}
func mustJSON(value any) jsonRawMessage { data, _ := json.Marshal(value); return data }
func bearerOrQuery(r *http.Request) string {
	if token := r.URL.Query().Get("token"); token != "" {
		return token
	}
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}
func publicBaseURL(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		scheme = forwarded
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func wsjsonRead(ctx context.Context, conn *websocket.Conn, value any) error {
	return wsjsonReadTimeout(ctx, conn, value, 0)
}
func wsjsonReadTimeout(ctx context.Context, conn *websocket.Conn, value any, timeout time.Duration) error {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	return wsjsonReadImpl(ctx, conn, value)
}
func wsjsonReadImpl(ctx context.Context, conn *websocket.Conn, value any) error {
	_, data, err := conn.Read(ctx)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}
func wsjsonWrite(ctx context.Context, conn *websocket.Conn, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, data)
}
