package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/BurntSushi/toml"
)

const defaultConfigPath = "config.toml"

type runtimeConfig struct {
	ConfigPath           string
	Addr                 string
	DBPath               string
	StatsEnabled         bool
	StatsPath            string
	StatsFlushInterval   time.Duration
	PairingTTL           time.Duration
	ShutdownTimeout      time.Duration
	ReadHeaderTimeout    time.Duration
	ConfigLoaded         bool
	ConfigLoadedFromPath string
}

type fileConfig struct {
	Server struct {
		Addr                     string `toml:"addr"`
		ReadHeaderTimeoutSeconds *int   `toml:"read_header_timeout_seconds"`
	} `toml:"server"`
	Database struct {
		Path string `toml:"path"`
	} `toml:"database"`
	Stats struct {
		Enabled              *bool  `toml:"enabled"`
		Path                 string `toml:"path"`
		FlushIntervalSeconds *int   `toml:"flush_interval_seconds"`
	} `toml:"stats"`
	Pairing struct {
		TTLSeconds *int `toml:"ttl_seconds"`
	} `toml:"pairing"`
	Shutdown struct {
		TimeoutSeconds *int `toml:"timeout_seconds"`
	} `toml:"shutdown"`
}

func loadRuntimeConfig(args []string) (runtimeConfig, error) {
	values := runtimeConfig{
		ConfigPath:         env("CODEX_SERVICE_CONFIG", defaultConfigPath),
		Addr:               ":8088",
		DBPath:             "codux-service.sqlite3",
		StatsEnabled:       true,
		StatsPath:          "codux-service.stats.jsonl",
		StatsFlushInterval: 10 * time.Second,
		PairingTTL:         300 * time.Second,
		ShutdownTimeout:    3 * time.Second,
		ReadHeaderTimeout:  10 * time.Second,
	}

	flagSet := flag.NewFlagSet("codux-service", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)
	configPath := flagSet.String("config", values.ConfigPath, "TOML config file path")
	addr := flagSet.String("addr", "", "HTTP/WebSocket listen address")
	dbPath := flagSet.String("db", "", "SQLite database path")
	statsEnabled := flagSet.Bool("stats", true, "enable relay statistics JSONL log")
	statsPath := flagSet.String("stats-path", "", "relay statistics JSONL path")
	statsFlushIntervalSeconds := flagSet.Int("stats-flush-interval", 0, "statistics snapshot interval in seconds")
	pairingTTLSeconds := flagSet.Int("pairing-ttl", 0, "pairing QR lifetime in seconds")
	shutdownTimeoutSeconds := flagSet.Int("shutdown-timeout", 0, "shutdown timeout in seconds")
	readHeaderTimeoutSeconds := flagSet.Int("read-header-timeout", 0, "HTTP read header timeout in seconds")
	if err := flagSet.Parse(args); err != nil {
		return values, err
	}

	values.ConfigPath = *configPath
	explicitFlags := visitedFlags(flagSet)
	configPathExplicit := explicitFlags["config"] || os.Getenv("CODEX_SERVICE_CONFIG") != ""
	if values.ConfigPath != "" {
		loaded, err := applyConfigFile(&values, values.ConfigPath, configPathExplicit)
		if err != nil {
			return values, err
		}
		values.ConfigLoaded = loaded
		if loaded {
			values.ConfigLoadedFromPath = values.ConfigPath
		}
	}

	if err := applyEnv(&values); err != nil {
		return values, err
	}
	if explicitFlags["addr"] {
		values.Addr = *addr
	}
	if explicitFlags["db"] {
		values.DBPath = *dbPath
	}
	if explicitFlags["stats"] {
		values.StatsEnabled = *statsEnabled
	}
	if explicitFlags["stats-path"] {
		values.StatsPath = *statsPath
	}
	if explicitFlags["stats-flush-interval"] {
		values.StatsFlushInterval = time.Duration(*statsFlushIntervalSeconds) * time.Second
	}
	if explicitFlags["pairing-ttl"] {
		values.PairingTTL = time.Duration(*pairingTTLSeconds) * time.Second
	}
	if explicitFlags["shutdown-timeout"] {
		values.ShutdownTimeout = time.Duration(*shutdownTimeoutSeconds) * time.Second
	}
	if explicitFlags["read-header-timeout"] {
		values.ReadHeaderTimeout = time.Duration(*readHeaderTimeoutSeconds) * time.Second
	}

	if err := values.validate(); err != nil {
		return values, err
	}
	return values, nil
}

func applyConfigFile(values *runtimeConfig, path string, required bool) (bool, error) {
	var cfg fileConfig
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		if errors.Is(err, os.ErrNotExist) && !required {
			return false, nil
		}
		return false, fmt.Errorf("load config %q: %w", path, err)
	}
	if cfg.Server.Addr != "" {
		values.Addr = cfg.Server.Addr
	}
	if cfg.Database.Path != "" {
		values.DBPath = cfg.Database.Path
	}
	if cfg.Stats.Enabled != nil {
		values.StatsEnabled = *cfg.Stats.Enabled
	}
	if cfg.Stats.Path != "" {
		values.StatsPath = cfg.Stats.Path
	}
	if cfg.Stats.FlushIntervalSeconds != nil {
		values.StatsFlushInterval = time.Duration(*cfg.Stats.FlushIntervalSeconds) * time.Second
	}
	if cfg.Pairing.TTLSeconds != nil {
		values.PairingTTL = time.Duration(*cfg.Pairing.TTLSeconds) * time.Second
	}
	if cfg.Shutdown.TimeoutSeconds != nil {
		values.ShutdownTimeout = time.Duration(*cfg.Shutdown.TimeoutSeconds) * time.Second
	}
	if cfg.Server.ReadHeaderTimeoutSeconds != nil {
		values.ReadHeaderTimeout = time.Duration(*cfg.Server.ReadHeaderTimeoutSeconds) * time.Second
	}
	return true, nil
}

func applyEnv(values *runtimeConfig) error {
	if value := os.Getenv("CODEX_SERVER_ADDR"); value != "" {
		values.Addr = value
	}
	if value := os.Getenv("CODEX_SERVER_DB"); value != "" {
		values.DBPath = value
	}
	if value, ok, err := envBool("CODEX_STATS_ENABLED"); err != nil {
		return err
	} else if ok {
		values.StatsEnabled = value
	}
	if value := os.Getenv("CODEX_STATS_PATH"); value != "" {
		values.StatsPath = value
	}
	if value, ok, err := envDurationSeconds("CODEX_STATS_FLUSH_INTERVAL"); err != nil {
		return err
	} else if ok {
		values.StatsFlushInterval = value
	}
	if value, ok, err := envDurationSeconds("CODEX_PAIRING_TTL"); err != nil {
		return err
	} else if ok {
		values.PairingTTL = value
	}
	if value, ok, err := envDurationSeconds("CODEX_SHUTDOWN_TIMEOUT"); err != nil {
		return err
	} else if ok {
		values.ShutdownTimeout = value
	}
	if value, ok, err := envDurationSeconds("CODEX_READ_HEADER_TIMEOUT"); err != nil {
		return err
	} else if ok {
		values.ReadHeaderTimeout = value
	}
	return nil
}

func envBool(key string) (bool, bool, error) {
	value := os.Getenv(key)
	if value == "" {
		return false, false, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, false, fmt.Errorf("%s must be a boolean value", key)
	}
	return parsed, true, nil
}

func envDurationSeconds(key string) (time.Duration, bool, error) {
	value := os.Getenv(key)
	if value == "" {
		return 0, false, nil
	}
	number, err := strconv.Atoi(value)
	if err != nil || number <= 0 {
		return 0, false, fmt.Errorf("%s must be a positive integer seconds value", key)
	}
	return time.Duration(number) * time.Second, true, nil
}

func visitedFlags(flagSet *flag.FlagSet) map[string]bool {
	visited := map[string]bool{}
	flagSet.Visit(func(flag *flag.Flag) {
		visited[flag.Name] = true
	})
	return visited
}

func (c runtimeConfig) validate() error {
	if c.Addr == "" {
		return fmt.Errorf("addr cannot be empty")
	}
	if c.DBPath == "" {
		return fmt.Errorf("db path cannot be empty")
	}
	if c.StatsEnabled && c.StatsPath == "" {
		return fmt.Errorf("stats path cannot be empty when stats are enabled")
	}
	if c.StatsEnabled && c.StatsFlushInterval <= 0 {
		return fmt.Errorf("stats flush interval must be positive")
	}
	if c.PairingTTL <= 0 {
		return fmt.Errorf("pairing ttl must be positive")
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("shutdown timeout must be positive")
	}
	if c.ReadHeaderTimeout <= 0 {
		return fmt.Errorf("read header timeout must be positive")
	}
	return nil
}
