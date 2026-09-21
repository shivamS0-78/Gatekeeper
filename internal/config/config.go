package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	Port int `yaml:"port"`
}

type Route struct {
	Path        string   `yaml:"path"`
	Upstreams   []string `yaml:"upstreams"`
	StripPrefix bool     `yaml:"strip_prefix"`
	Retries     int      `yaml:"retries"`
}

type Config struct {
	Server ServerConfig
	Routes []Route
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)

	if err != nil {
		return nil, err
	}

	var config Config

	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return &config, nil
}
