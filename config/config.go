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
	}

	Application struct {
		Name string
		Env  string
		Port string
	}
)

func LoadConfiguration(fileName string) (*Config, error) {
	err := godotenv.Load(fileName)
	if err != nil {
		return nil, fmt.Errorf("error loading .env file: %w", err)
	}

	app := Application{
		Name: GetEnv("APP_NAME", "zolo-test-integration"),
		Env:  GetEnv("APP_ENV", "development"),
		Port: GetEnv("APP_PORT", "8080"),
	}

	config := &Config{
		Application: app,
	}

	return config, nil
}

func GetEnv(key string, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}

	return defaultVal
}

func getEnvAsInt(name string, defaultVal int) int {
	valStr := GetEnv(name, "")
	if value, err := strconv.Atoi(valStr); err == nil {
		return value
	}

	return defaultVal
}
