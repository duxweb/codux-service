package server

import "time"

type response map[string]any

type registerHostRequest struct {
	HostID    string `json:"hostId"`
	Name      string `json:"name"`
	Token     string `json:"token"`
	PublicKey string `json:"publicKey"`
}

type registerHostResponse struct {
	HostID string `json:"hostId"`
	Token  string `json:"token"`
}

type createPairingRequest struct {
	HostID string `json:"hostId"`
	Token  string `json:"token"`
}

type createPairingResponse struct {
	PairingID     string    `json:"pairingId"`
	Code          string    `json:"code"`
	Secret        string    `json:"secret"`
	HostName      string    `json:"hostName,omitempty"`
	HostPublicKey string    `json:"hostPublicKey,omitempty"`
	CryptoVersion int       `json:"cryptoVersion,omitempty"`
	ExpiresAt     time.Time `json:"expiresAt"`
	QRPayload     string    `json:"qrPayload"`
}

type claimPairingRequest struct {
	Code      string `json:"code"`
	Secret    string `json:"secret"`
	Name      string `json:"name"`
	PublicKey string `json:"publicKey"`
}

type claimPairingResponse struct {
	PairingID string `json:"pairingId"`
	HostID    string `json:"hostId"`
	Status    string `json:"status"`
}

type pairingStatusRequest struct {
	Code   string `json:"code"`
	Secret string `json:"secret"`
}

type pairingStatusResponse struct {
	Status          string `json:"status"`
	PairingID       string `json:"pairingId,omitempty"`
	HostID          string `json:"hostId"`
	HostName        string `json:"hostName,omitempty"`
	HostPublicKey   string `json:"hostPublicKey,omitempty"`
	CryptoVersion   int    `json:"cryptoVersion,omitempty"`
	Code            string `json:"code,omitempty"`
	DeviceName      string `json:"deviceName,omitempty"`
	DevicePublicKey string `json:"devicePublicKey,omitempty"`
	DeviceID        string `json:"deviceId,omitempty"`
	Token           string `json:"token,omitempty"`
}

type confirmPairingRequest struct {
	HostID    string `json:"hostId"`
	Token     string `json:"token"`
	PairingID string `json:"pairingId"`
}

type rejectPairingRequest struct {
	HostID    string `json:"hostId"`
	Token     string `json:"token"`
	PairingID string `json:"pairingId"`
}

type confirmPairingResponse struct {
	DeviceID string `json:"deviceId"`
	HostID   string `json:"hostId"`
	Token    string `json:"token"`
}

type revokeDeviceRequest struct {
	HostID   string `json:"hostId"`
	Token    string `json:"token"`
	DeviceID string `json:"deviceId"`
}

type envelope struct {
	Type      string         `json:"type"`
	ID        string         `json:"id,omitempty"`
	HostID    string         `json:"hostId,omitempty"`
	DeviceID  string         `json:"deviceId,omitempty"`
	SessionID string         `json:"sessionId,omitempty"`
	Seq       int64          `json:"seq,omitempty"`
	Payload   jsonRawMessage `json:"payload,omitempty"`
	Error     string         `json:"error,omitempty"`
	At        int64          `json:"at,omitempty"`
}

type jsonRawMessage []byte

func (m jsonRawMessage) MarshalJSON() ([]byte, error) {
	if len(m) == 0 {
		return []byte("null"), nil
	}
	return m, nil
}

func (m *jsonRawMessage) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*m = nil
		return nil
	}
	*m = append((*m)[0:0], data...)
	return nil
}
