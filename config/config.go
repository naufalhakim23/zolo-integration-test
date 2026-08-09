package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type (
	Config struct {
		Application Application
		Database    Database
		ERP         ERP
	}

	Application struct {
		Name            string
		Env             string
		Port            string
		DefaultLanguage string
	}

	Database struct {
		Path string
	}

	ERP struct {
		AlphaBaseURL string
		BetaBaseURL  string
		Timeout      time.Duration
		MaxAttempts  int
		BackoffBase  time.Duration
		BackoffMax   time.Duration
	}
)

func LoadConfiguration(fileName string) (*Config, error) {
	err := godotenv.Load(fileName)
	if err != nil {
		return nil, fmt.Errorf("error loading .env file: %w", err)
	}

	app := Application{
		Name:            GetEnv("APP_NAME", "zolo-test-integration"),
		Env:             GetEnv("APP_ENV", "development"),
		Port:            GetEnv("APP_PORT", "8080"),
		DefaultLanguage: GetEnv("APP_DEFAULT_LANGUAGE", "en"),
	}

	database := Database{
		Path: GetEnv("DB_PATH", "zolo.db"),
	}

	erp := ERP{
		AlphaBaseURL: GetEnv("ERP_ALPHA_BASE_URL", "http://localhost:9090"),
		BetaBaseURL:  GetEnv("ERP_BETA_BASE_URL", "http://localhost:9090"),
		Timeout:      getEnvAsDuration("ERP_TIMEOUT", 10*time.Second),
		MaxAttempts:  getEnvAsInt("ERP_MAX_ATTEMPTS", 4),
		BackoffBase:  getEnvAsDuration("ERP_BACKOFF_BASE", 100*time.Millisecond),
		BackoffMax:   getEnvAsDuration("ERP_BACKOFF_MAX", 2*time.Second),
	}

	return &Config{
		Application: app,
		Database:    database,
		ERP:         erp,
	}, nil
}

func GetEnv(key string, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}

	return defaultVal
}

func getEnvAsInt(name string, defaultVal int) int {
	if value, err := strconv.Atoi(GetEnv(name, "")); err == nil {
		return value
	}

	return defaultVal
}

func getEnvAsDuration(name string, defaultVal time.Duration) time.Duration {
	if value, err := time.ParseDuration(GetEnv(name, "")); err == nil {
		return value
	}

	return defaultVal
}
