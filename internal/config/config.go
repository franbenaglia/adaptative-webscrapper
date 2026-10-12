// Package config lee la configuración de variables de entorno y, si existe,
// de un archivo .env local. Las variables de entorno tienen prioridad.
package config

import (
	"bufio"
	"fmt"
	"net/url"
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
	WebDir            string        // APP_WEB_DIR

	AIEnabled     bool          // AI_ENABLED
	OllamaBaseURL string        // OLLAMA_BASE_URL
	OllamaModel   string        // OLLAMA_MODEL
	AIMaxRepairs  int           // AI_MAX_REPAIRS
	AITimeout     time.Duration // AI_TIMEOUT
}

// DefaultOllamaModel es el modelo usado si no se configura OLLAMA_MODEL.
const DefaultOllamaModel = "llama3.2:3b"

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
		WebDir:   get("APP_WEB_DIR", filepath.Join("web", "dist")),

		OllamaBaseURL: get("OLLAMA_BASE_URL", "http://127.0.0.1:11434"),
		OllamaModel:   get("OLLAMA_MODEL", DefaultOllamaModel),
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
	if cfg.AIEnabled, err = strconv.ParseBool(get("AI_ENABLED", "false")); err != nil {
		return cfg, fmt.Errorf("AI_ENABLED debe ser true o false")
	}
	if cfg.AIMaxRepairs, err = strconv.Atoi(get("AI_MAX_REPAIRS", "2")); err != nil || cfg.AIMaxRepairs < 0 || cfg.AIMaxRepairs > 5 {
		return cfg, fmt.Errorf("AI_MAX_REPAIRS debe ser un entero entre 0 y 5")
	}
	// Generoso por defecto: sin GPU, un modelo pequeño tarda del orden de un minuto.
	if cfg.AITimeout, err = time.ParseDuration(get("AI_TIMEOUT", "5m")); err != nil || cfg.AITimeout <= 0 {
		return cfg, fmt.Errorf("AI_TIMEOUT debe ser una duración como 5m o 90s")
	}
	if u, err := url.Parse(cfg.OllamaBaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return cfg, fmt.Errorf("OLLAMA_BASE_URL debe ser una URL http o https")
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
