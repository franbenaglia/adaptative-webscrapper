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
	"adaptive-scraper/internal/storage/sqlite"
)

// App agrupa los servicios y los recursos que hay que cerrar al terminar.
type App struct {
	Config  config.Config
	Jobs    *application.JobService
	Recipes *application.RecipeService

	store    *sqlite.Store
	launcher *browser.Launcher
}

// Open abre la base de datos y construye los servicios. El navegador no se
// inicia hasta el primer trabajo.
func Open(ctx context.Context, cfg config.Config, logger *slog.Logger) (*App, error) {
	store, err := sqlite.Open(ctx, cfg.DatabasePath)
	if err != nil {
		return nil, err
	}
	launcher := browser.NewLauncher()
	return &App{
		Config: cfg,
		Jobs: application.NewJobService(application.NewService(launcher), store.Jobs(), store.Recipes(),
			application.JobOptions{Logger: logger, Timeout: cfg.JobTimeout}),
		Recipes:  application.NewRecipeService(store.Recipes()),
		store:    store,
		launcher: launcher,
	}, nil
}

func (a *App) Close() error {
	return errors.Join(a.launcher.Close(), a.store.Close())
}
