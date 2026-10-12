// Package bootstrap es la raíz de composición: conecta los adaptadores reales
// con los servicios de aplicación. Lo usan por igual cmd/server y cmd/scraper,
// de modo que ambos ejecutan exactamente el mismo motor.
package bootstrap

import (
	"context"
	"errors"
	"log/slog"

	"adaptive-scraper/internal/application"
	"adaptive-scraper/internal/browser"
	"adaptive-scraper/internal/config"
	"adaptive-scraper/internal/intelligence"
	"adaptive-scraper/internal/netguard"
	"adaptive-scraper/internal/storage/sqlite"
)

// App agrupa los servicios y los recursos que hay que cerrar al terminar.
type App struct {
	Config  config.Config
	Jobs    *application.JobService
	Recipes *application.RecipeService
	// AIStatus informa del proveedor de IA configurado.
	AIStatus func(ctx context.Context) application.AIStatus

	store    *sqlite.Store
	launcher *browser.Launcher
}

// Open abre la base de datos y construye los servicios. El navegador no se
// inicia hasta el primer trabajo y el modelo no se contacta hasta que hace falta.
func Open(ctx context.Context, cfg config.Config, logger *slog.Logger) (*App, error) {
	store, err := sqlite.Open(ctx, cfg.DatabasePath)
	if err != nil {
		return nil, err
	}
	launcher := browser.NewLauncher(browser.Options{
		Policy:                netguard.Policy{AllowPrivate: cfg.AllowPrivateNetworks},
		MaxBytesPerConnection: cfg.MaxResponseBytes,
	})

	engineOptions := application.ServiceOptions{MaxRepairs: cfg.AIMaxRepairs}
	aiStatus := func(context.Context) application.AIStatus {
		return application.AIStatus{InstalledModels: []string{}, Message: "La IA está desactivada. Actívala con AI_ENABLED=true."}
	}
	if cfg.AIEnabled {
		ollama := intelligence.NewOllama(cfg.OllamaBaseURL, cfg.OllamaModel, cfg.AITimeout)
		engineOptions.Intelligence = intelligence.NewProvider(ollama)
		aiStatus = ollama.Status
	}

	return &App{
		Config: cfg,
		Jobs: application.NewJobService(application.NewService(launcher, engineOptions), store.Jobs(), store.Recipes(),
			application.JobOptions{Logger: logger, Timeout: cfg.JobTimeout, AIEnabled: cfg.AIEnabled, DiagnosticsDir: cfg.DiagnosticsDir()}),
		Recipes:  application.NewRecipeService(store.Recipes()),
		AIStatus: aiStatus,
		store:    store,
		launcher: launcher,
	}, nil
}

func (a *App) Close() error {
	return errors.Join(a.launcher.Close(), a.store.Close())
}
