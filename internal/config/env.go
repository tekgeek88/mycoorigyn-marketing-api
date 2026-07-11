package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

func init() {
	env := os.Getenv("ENV")
	if env == "" {
		env = "development"
	}

	// Only load .env files locally. Containerized environments should provide env vars directly.
	if env == "staging" || env == "production" {
		log.Printf("ENV=%s, skipping godotenv", env)
		return
	}

	if err := godotenv.Load(); err != nil {
		log.Printf("Could not load .env: %v", err)
		return
	}

	log.Printf("Loaded environment config from .env")
}
