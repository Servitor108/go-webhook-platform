package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	DatabaseURL string
	HTTPAddr string
	WorkerConcurrency int
	RequestTimeout time.Duration
	MaxAttempts int
}

func Load() Config {
	return Config{
		DatabaseURL: env("DATABASE_URL", "postgres://webhooks:webhooks@localhost:5432/webhooks?sslmode=disable"),
		HTTPAddr: env("HTTP_ADDR", ":8080"),
		WorkerConcurrency: integer("WORKER_CONCURRENCY", 8),
		RequestTimeout: time.Duration(integer("REQUEST_TIMEOUT_SECONDS", 10))*time.Second,
		MaxAttempts: integer("MAX_ATTEMPTS", 8),
	}
}
func env(key, fallback string) string { if v:=os.Getenv(key); v!="" { return v }; return fallback }
func integer(key string, fallback int) int { v,err:=strconv.Atoi(os.Getenv(key)); if err!=nil || v<1 { return fallback }; return v }
