package config

import "time"

type Config struct {
	ShutdownTimeout time.Duration
	// TODO
}

func Load() (Config, error) {
	// TODO
	return Config{}, nil
}
