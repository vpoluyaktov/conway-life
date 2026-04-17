package config

import (
	"log"
	"os"
	"strconv"
)

type Config struct {
	Port                  string
	Version               string
	Environment           string
	ProjectID             string
	FirestoreDatabaseName string
	MaxBoardWidth         int
	MaxBoardHeight        int
	MaxSessionCells       int
}

func Load() *Config {
	return &Config{
		Port:                  getEnv("PORT", "8080"),
		Version:               getEnv("APP_VERSION", "dev"),
		Environment:           getEnv("ENVIRONMENT", "local"),
		ProjectID:             getEnv("GCP_PROJECT_ID", ""),
		FirestoreDatabaseName: getEnv("FIRESTORE_DATABASE_NAME", "(default)"),
		MaxBoardWidth:         getEnvInt("MAX_BOARD_WIDTH", 200),
		MaxBoardHeight:        getEnvInt("MAX_BOARD_HEIGHT", 200),
		MaxSessionCells:       getEnvInt("MAX_SESSION_CELLS", 2000000),
	}
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			log.Printf("warning: invalid value for %s=%q, using default %d", key, v, defaultVal)
			return defaultVal
		}
		return n
	}
	return defaultVal
}
