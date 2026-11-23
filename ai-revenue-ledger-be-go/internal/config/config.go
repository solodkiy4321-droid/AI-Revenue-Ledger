package config

import (
	"os"
	"time"
)

type Config struct {
	Port        string
	DatabaseURL string
	DBTimeout   time.Duration
}

func Load() Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	return Config{
		Port:        port,
		DatabaseURL: os.Getenv("DATABASE_URL"),
		DBTimeout:   5 * time.Second,
	}
}