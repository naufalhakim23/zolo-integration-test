package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type (
	Config struct {
		Application Application
		Database    Database
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

	return &Config{
		Application: app,
		Database:    database,
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
