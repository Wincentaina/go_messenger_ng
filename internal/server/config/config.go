// Package config loads and holds the server configuration.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		Host           string `yaml:"host"`
		Port           int    `yaml:"port"`
		MaxConnections int    `yaml:"max_connections"`
	} `yaml:"server"`

	TLS struct {
		CertFile string `yaml:"cert_file"`
		KeyFile  string `yaml:"key_file"`
	} `yaml:"tls"`

	Database struct {
		DSN          string `yaml:"dsn"`
		MaxOpenConns int    `yaml:"max_open_conns"`
		MaxIdleConns int    `yaml:"max_idle_conns"`
	} `yaml:"database"`

	Logging struct {
		File  string `yaml:"file"`
		Level string `yaml:"level"`
	} `yaml:"logging"`
}

// Load reads and parses a YAML config file.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	// Allow overriding DSN via environment variable (useful in Docker)
	if dsn := os.Getenv("MESSENGER_DB_DSN"); dsn != "" {
		cfg.Database.DSN = dsn
	}
	return cfg, nil
}
