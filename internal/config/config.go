// Package config lee la configuración de variables de entorno y, si existe,
// de un archivo .env local. Las variables de entorno tienen prioridad.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config es la configuración de la aplicación.
type Config struct {
	HTTPAddr          string        // APP_HTTP_ADDR
	DataDir           string        // APP_DATA_DIR
	DatabasePath      string        // APP_DATABASE_PATH
	MaxConcurrentJobs int           // APP_MAX_CONCURRENT_JOBS
	JobTimeout        time.Duration // APP_JOB_TIMEOUT
	Headless          bool          // PLAYWRIGHT_HEADLESS
}

// Load lee la configuración del entorno y de ./.env si existe.
func Load() (Config, error) {
	file, err := os.ReadFile(".env")
	if err != nil && !os.IsNotExist(err) {
		return Config{}, err
	}
	return load(os.LookupEnv, string(file))
}

func load(lookupEnv func(string) (string, bool), envFile string) (Config, error) {
	fileValues := parseEnvFile(envFile)
	get := func(name, fallback string) string {
		if v, ok := lookupEnv(name); ok && v != "" {
			return v
		}
		if v, ok := fileValues[name]; ok && v != "" {
			return v
		}
		return fallback
	}

	cfg := Config{
		// Solo loopback por defecto: la API no tiene autenticación.
		HTTPAddr: get("APP_HTTP_ADDR", "127.0.0.1:8080"),
		DataDir:  get("APP_DATA_DIR", "data"),
	}
	cfg.DatabasePath = get("APP_DATABASE_PATH", filepath.Join(cfg.DataDir, "adaptive-scraper.db"))

	var err error
	if cfg.MaxConcurrentJobs, err = strconv.Atoi(get("APP_MAX_CONCURRENT_JOBS", "2")); err != nil || cfg.MaxConcurrentJobs < 1 {
		return cfg, fmt.Errorf("APP_MAX_CONCURRENT_JOBS debe ser un entero mayor que cero")
	}
	if cfg.JobTimeout, err = time.ParseDuration(get("APP_JOB_TIMEOUT", "10m")); err != nil || cfg.JobTimeout < 0 {
		return cfg, fmt.Errorf("APP_JOB_TIMEOUT debe ser una duración como 10m o 90s")
	}
	if cfg.Headless, err = strconv.ParseBool(get("PLAYWRIGHT_HEADLESS", "true")); err != nil {
		return cfg, fmt.Errorf("PLAYWRIGHT_HEADLESS debe ser true o false")
	}
	return cfg, nil
}

// parseEnvFile admite líneas CLAVE=valor, comentarios con # y comillas opcionales.
func parseEnvFile(content string) map[string]string {
	values := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		values[strings.TrimSpace(key)] = value
	}
	return values
}
