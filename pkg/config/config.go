package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Main MainConfig `yaml:"Main"`
}

type MainConfig struct {
	Endpoint    string  `yaml:"endpoint"`
	Username    string  `yaml:"username"`
	Password    string  `yaml:"password"`
	AuthToken   *string `yaml:"authToken"`
	FilesFolder string  `yaml:"filesLocalFolder"`
}

func LoadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	var cfg Config
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}
