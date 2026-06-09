package server

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const defaultStatsFlushInterval = 10 * time.Second

type StatsRecorder struct {
	logger        *slog.Logger
	path          string
	flushInterval time.Duration
	mu            sync.Mutex
	file          *os.File
	lastFlush     time.Time
	counters      relayCounters
}

type relayCounters struct {
	ConnectionsTotal    uint64 `json:"connectionsTotal"`
	DisconnectionsTotal uint64 `json:"disconnectionsTotal"`
	MessagesTotal       uint64 `json:"messagesTotal"`
	MessagesForwarded   uint64 `json:"messagesForwarded"`
	MessagesDropped     uint64 `json:"messagesDropped"`
	BytesTotal          uint64 `json:"bytesTotal"`
	RateLimitedTotal    uint64 `json:"rateLimitedTotal"`
	OversizedTotal      uint64 `json:"oversizedTotal"`
	UploadBlockedTotal  uint64 `json:"uploadBlockedTotal"`
}

type statsEntry struct {
	Time     string         `json:"time"`
	Event    string         `json:"event"`
	Protocol string         `json:"protocol,omitempty"`
	Role     string         `json:"role,omitempty"`
	HostID   string         `json:"hostId,omitempty"`
	DeviceID string         `json:"deviceId,omitempty"`
	Type     string         `json:"type,omitempty"`
	Reason   string         `json:"reason,omitempty"`
	Bytes    int64          `json:"bytes,omitempty"`
	Counters *relayCounters `json:"counters,omitempty"`
}

func NewStatsRecorder(path string, flushInterval time.Duration, logger *slog.Logger) (*StatsRecorder, error) {
	path = filepath.Clean(path)
	if flushInterval <= 0 {
		flushInterval = defaultStatsFlushInterval
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &StatsRecorder{
		logger:        logger,
		path:          path,
		flushInterval: flushInterval,
		file:          file,
		lastFlush:     time.Now(),
	}, nil
}

func (s *StatsRecorder) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeLocked(statsEntry{Event: "snapshot", Counters: s.countersSnapshotLocked()})
	if s.file == nil {
		return nil
	}
	return s.file.Close()
}

func (s *StatsRecorder) RecordConnect(p *peer) {
	if s == nil || p == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counters.ConnectionsTotal++
	s.writeLocked(statsEntry{
		Event:    "connect",
		Protocol: peerProtocol(p),
		Role:     p.role,
		HostID:   p.hostID,
		DeviceID: p.deviceID,
	})
	s.maybeSnapshotLocked()
}

func (s *StatsRecorder) RecordDisconnect(p *peer) {
	if s == nil || p == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counters.DisconnectionsTotal++
	s.writeLocked(statsEntry{
		Event:    "disconnect",
		Protocol: peerProtocol(p),
		Role:     p.role,
		HostID:   p.hostID,
		DeviceID: p.deviceID,
	})
	s.maybeSnapshotLocked()
}

func (s *StatsRecorder) RecordMessage(p *peer, msg envelope, size int64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counters.MessagesTotal++
	if size > 0 {
		s.counters.BytesTotal += uint64(size)
	}
	s.maybeSnapshotLocked()
}

func (s *StatsRecorder) RecordForwarded(p *peer, msg envelope, count int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if count > 0 {
		s.counters.MessagesForwarded += uint64(count)
	}
	s.maybeSnapshotLocked()
}

func (s *StatsRecorder) RecordDropped(p *peer, msg envelope, reason string, size int64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counters.MessagesDropped++
	switch reason {
	case "rate_limited":
		s.counters.RateLimitedTotal++
	case "message_too_large":
		s.counters.OversizedTotal++
	case "upload_requires_p2p":
		s.counters.UploadBlockedTotal++
	}
	s.writeLocked(statsEntry{
		Event:    "drop",
		Protocol: peerProtocol(p),
		Role:     p.role,
		HostID:   p.hostID,
		DeviceID: p.deviceID,
		Type:     msg.Type,
		Reason:   reason,
		Bytes:    size,
	})
	s.maybeSnapshotLocked()
}

func (s *StatsRecorder) maybeSnapshotLocked() {
	if time.Since(s.lastFlush) < s.flushInterval {
		return
	}
	s.lastFlush = time.Now()
	s.writeLocked(statsEntry{Event: "snapshot", Counters: s.countersSnapshotLocked()})
}

func (s *StatsRecorder) countersSnapshotLocked() *relayCounters {
	snapshot := s.counters
	return &snapshot
}

func (s *StatsRecorder) writeLocked(entry statsEntry) {
	if s.file == nil {
		return
	}
	entry.Time = time.Now().UTC().Format(time.RFC3339Nano)
	data, err := json.Marshal(entry)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("stats encode failed", "error", err)
		}
		return
	}
	if _, err := s.file.Write(append(data, '\n')); err != nil && s.logger != nil {
		s.logger.Warn("stats write failed", "path", s.path, "error", err)
	}
}

func peerProtocol(p *peer) string {
	if p != nil && p.stateless {
		return "v3"
	}
	return "legacy"
}
