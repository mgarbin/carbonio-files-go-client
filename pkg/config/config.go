package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Main    MainConfig    `yaml:"Main"`
	Logging LoggingConfig `yaml:"Logging"`
}

type MainConfig struct {
	Endpoint    string  `yaml:"endpoint"`
	Username    string  `yaml:"username"`
	Password    string  `yaml:"password"`
	AuthToken   *string `yaml:"authToken"`
	FilesFolder string  `yaml:"filesLocalFolder"`
}

// LoggingConfig configures the zerolog-backed logger (see pkg/logger).
// Every field is optional: an empty value falls back to logger.Default().
type LoggingConfig struct {
	// Level is a zerolog level name: trace, debug, info, warn, error, fatal,
	// panic, disabled. Default "info".
	Level string `yaml:"level"`
	// Format is "console" (human-readable) or "json". Default "console".
	Format string `yaml:"format"`
	// Output is "console", "file" or "both". Default "console".
	Output string `yaml:"output"`
	// Path is the log file path, used when Output is "file" or "both".
	// Default "<home>/.carbonio_files_sync/carbonio-files-go-client.log".
	Path string `yaml:"path"`
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
