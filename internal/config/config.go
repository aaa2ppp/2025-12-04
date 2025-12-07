package config

import (
	"link-checker/internal/server"
)

type Config struct {
	Server server.Config
	// TODO
}

func Load() (Config, error) {
	// TODO
	return Config{}, nil
}
