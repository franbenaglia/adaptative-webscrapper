package config

import (
	"path/filepath"
	"testing"
	"time"
)

func env(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := values[name]
		return v, ok
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := load(env(nil), "")
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		HTTPAddr: "127.0.0.1:8080", DataDir: "data", DatabasePath: filepath.Join("data", "adaptive-scraper.db"),
		MaxConcurrentJobs: 2, JobTimeout: 10 * time.Minute, Headless: true, WebDir: filepath.Join("web", "dist"),
		OllamaBaseURL: "http://127.0.0.1:11434", OllamaModel: DefaultOllamaModel, AIMaxRepairs: 2, AITimeout: 5 * time.Minute,
	}
	if cfg != want {
		t.Errorf("config = %+v", cfg)
	}
}

func TestLoadEnvOverridesFile(t *testing.T) {
	file := `
# comentario
APP_DATA_DIR = "/var/lib/scraper"
APP_MAX_CONCURRENT_JOBS=4
PLAYWRIGHT_HEADLESS='false'
línea sin igual
`
	cfg, err := load(env(map[string]string{"APP_MAX_CONCURRENT_JOBS": "8", "APP_JOB_TIMEOUT": "90s", "AI_ENABLED": "true", "OLLAMA_MODEL": "otro:1b"}), file)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != "/var/lib/scraper" || cfg.DatabasePath != "/var/lib/scraper/adaptive-scraper.db" ||
		cfg.MaxConcurrentJobs != 8 || cfg.JobTimeout != 90*time.Second || cfg.Headless || !cfg.AIEnabled || cfg.OllamaModel != "otro:1b" {
		t.Errorf("config = %+v", cfg)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	for name, value := range map[string]string{
		"APP_MAX_CONCURRENT_JOBS": "0",
		"APP_JOB_TIMEOUT":         "pronto",
		"PLAYWRIGHT_HEADLESS":     "quizás",
		"AI_ENABLED":              "a veces",
		"AI_MAX_REPAIRS":          "50",
		"AI_TIMEOUT":              "0s",
		"OLLAMA_BASE_URL":         "localhost:11434",
	} {
		if _, err := load(env(map[string]string{name: value}), ""); err == nil {
			t.Errorf("%s=%s debería rechazarse", name, value)
		}
	}
}
