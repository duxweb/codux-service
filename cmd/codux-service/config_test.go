package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadRuntimeConfigUsesDefaultsWithoutConfigFile(t *testing.T) {
	t.Setenv("CODEX_SERVICE_CONFIG", "")
	config, err := loadRuntimeConfig(nil)
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}

	if config.ConfigLoaded {
		t.Fatalf("expected no default config to be loaded")
	}
	if config.Addr != ":8088" {
		t.Fatalf("unexpected addr: %s", config.Addr)
	}
	if config.DBPath != "codux-service.sqlite3" {
		t.Fatalf("unexpected db path: %s", config.DBPath)
	}
	if config.PairingTTL != 300*time.Second {
		t.Fatalf("unexpected pairing ttl: %s", config.PairingTTL)
	}
}

func TestLoadRuntimeConfigReadsTomlConfig(t *testing.T) {
	configPath := writeConfig(t, `
[server]
addr = "127.0.0.1:19088"
read_header_timeout_seconds = 12

[database]
path = "/tmp/codux-service.sqlite3"

[pairing]
ttl_seconds = 120

[shutdown]
timeout_seconds = 4
`)

	config, err := loadRuntimeConfig([]string{"-config", configPath})
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}

	if !config.ConfigLoaded {
		t.Fatalf("expected config to be loaded")
	}
	if config.Addr != "127.0.0.1:19088" {
		t.Fatalf("unexpected addr: %s", config.Addr)
	}
	if config.DBPath != "/tmp/codux-service.sqlite3" {
		t.Fatalf("unexpected db path: %s", config.DBPath)
	}
	if config.PairingTTL != 120*time.Second {
		t.Fatalf("unexpected pairing ttl: %s", config.PairingTTL)
	}
	if config.ShutdownTimeout != 4*time.Second {
		t.Fatalf("unexpected shutdown timeout: %s", config.ShutdownTimeout)
	}
	if config.ReadHeaderTimeout != 12*time.Second {
		t.Fatalf("unexpected read header timeout: %s", config.ReadHeaderTimeout)
	}
}

func TestLoadRuntimeConfigOverrideOrder(t *testing.T) {
	configPath := writeConfig(t, `
[server]
addr = "127.0.0.1:10000"

[database]
path = "config.sqlite3"

[pairing]
ttl_seconds = 100
`)
	t.Setenv("CODEX_SERVER_ADDR", "127.0.0.1:20000")
	t.Setenv("CODEX_SERVER_DB", "env.sqlite3")
	t.Setenv("CODEX_PAIRING_TTL", "200")

	config, err := loadRuntimeConfig([]string{
		"-config", configPath,
		"-addr", "127.0.0.1:30000",
		"-db", "flag.sqlite3",
		"-pairing-ttl", "300",
	})
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}

	if config.Addr != "127.0.0.1:30000" {
		t.Fatalf("expected flag addr, got %s", config.Addr)
	}
	if config.DBPath != "flag.sqlite3" {
		t.Fatalf("expected flag db path, got %s", config.DBPath)
	}
	if config.PairingTTL != 300*time.Second {
		t.Fatalf("expected flag pairing ttl, got %s", config.PairingTTL)
	}
}

func TestLoadRuntimeConfigRejectsInvalidTomlDuration(t *testing.T) {
	configPath := writeConfig(t, `
[pairing]
ttl_seconds = 0
`)

	if _, err := loadRuntimeConfig([]string{"-config", configPath}); err == nil {
		t.Fatalf("expected invalid pairing ttl error")
	}
}

func TestLoadRuntimeConfigRejectsInvalidEnvDuration(t *testing.T) {
	t.Setenv("CODEX_SERVICE_CONFIG", "")
	t.Setenv("CODEX_PAIRING_TTL", "bad")

	if _, err := loadRuntimeConfig(nil); err == nil {
		t.Fatalf("expected invalid env duration error")
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
