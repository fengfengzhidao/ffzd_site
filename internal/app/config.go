package app

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	Addr, DatabasePath, UploadDir string
	AdminUsername, AdminPassword  string
	SessionSecret                 string
}

func LoadConfig() (Config, error) {
	c := Config{
		Addr:          env("APP_ADDR", "127.0.0.1:8080"),
		DatabasePath:  env("DATABASE_PATH", "data/blog.db"),
		UploadDir:     env("UPLOAD_DIR", "uploads"),
		AdminUsername: os.Getenv("ADMIN_USERNAME"),
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),
		SessionSecret: os.Getenv("SESSION_SECRET"),
	}
	if c.SessionSecret == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return c, fmt.Errorf("生成会话密钥: %w", err)
		}
		c.SessionSecret = hex.EncodeToString(b)
	}
	for _, dir := range []string{filepath.Dir(c.DatabasePath), c.UploadDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return c, err
		}
	}
	return c, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
