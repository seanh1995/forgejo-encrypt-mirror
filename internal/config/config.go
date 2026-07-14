package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		Address string `yaml:"address"`
	} `yaml:"server"`

	Forgejo struct {
		URL            string `yaml:"url"`
		Token          string `yaml:"token"`
		WebhookSecret  string `yaml:"webhookSecret"`
	} `yaml:"forgejo"`

	GitHub struct {
		Owner      string `yaml:"owner"`
		Token      string `yaml:"token"`
		AutoCreate bool   `yaml:"autoCreate"`
	} `yaml:"github"`

	Encryption struct {
		Recipient string `yaml:"recipient"`
	} `yaml:"encryption"`

	Git struct {
		CacheDir string `yaml:"cacheDir"`
	} `yaml:"git"`
}

func Load(path string) (*Config, error) {

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config

	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}

	if cfg.Git.CacheDir == "" {
		cfg.Git.CacheDir = "cache"
	}

	return &cfg, nil
}