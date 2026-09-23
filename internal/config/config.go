package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config stores the database and application configuration.
type Config struct {
	Database DatabaseConfig `json:"database"`
	API      APIConfig      `json:"api"`
}

// DatabaseConfig stores PostgreSQL connection parameters.
type DatabaseConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	DBName   string `json:"dbname"`
	SSLMode  string `json:"sslmode"`
}

// APIConfig stores the configuration for REST API integration.
type APIConfig struct {
	BaseURL     string `json:"base_url"`
	BearerToken string `json:"bearer_token"`
}

// ConnectionString generates a DSN for PostgreSQL connections.
func (d DatabaseConfig) ConnectionString() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		d.Host, d.Port, d.User, d.Password, d.DBName, d.SSLMode,
	)
}

// Load reads the configuration from a JSON file or environment variables.
// Priority: environment variables > config file > defaults.
func Load(configPath string) (*Config, error) {
	cfg := &Config{
		Database: DatabaseConfig{
			Host:    "localhost",
			Port:    5432,
			SSLMode: "disable",
		},
	}

	// Read from config file if a path is provided
	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
		}
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}
	}

	// Override with environment variables if present
	if v := os.Getenv("DB_HOST"); v != "" {
		cfg.Database.Host = v
	}
	if v := os.Getenv("DB_USER"); v != "" {
		cfg.Database.User = v
	}
	if v := os.Getenv("DB_PASSWORD"); v != "" {
		cfg.Database.Password = v
	}
	if v := os.Getenv("DB_NAME"); v != "" {
		cfg.Database.DBName = v
	}
	if v := os.Getenv("DB_SSLMODE"); v != "" {
		cfg.Database.SSLMode = v
	}
	if v := os.Getenv("API_BASE_URL"); v != "" {
		cfg.API.BaseURL = v
	}
	if v := os.Getenv("API_BEARER_TOKEN"); v != "" {
		cfg.API.BearerToken = v
	}

	// Minimal validation
	if cfg.Database.User == "" || cfg.Database.DBName == "" {
		return nil, fmt.Errorf("DB_USER and DB_NAME are required (via config file or environment variable)")
	}

	return cfg, nil
}
