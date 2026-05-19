// Package config loads client configuration from a YAML file.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		Address string `yaml:"address"`
	} `yaml:"server"`

	TLS struct {
		CACert     string `yaml:"ca_cert"`
		SkipVerify bool   `yaml:"skip_verify"`
	} `yaml:"tls"`

	Cache struct {
		MaxMessagesPerChat int `yaml:"max_messages_per_chat"`
	} `yaml:"cache"`
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}
