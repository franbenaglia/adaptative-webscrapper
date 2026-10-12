// Comando adaptive-scraper: solo cablea dependencias y delega en internal/cli.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"adaptive-scraper/internal/bootstrap"
	"adaptive-scraper/internal/cli"
	"adaptive-scraper/internal/config"
)

// version se sobrescribe con -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error de configuración:", err)
		return cli.ExitError
	}
	// El progreso lo imprime la CLI; el log solo muestra problemas.
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	root := cli.NewRootCommand(cli.Deps{
		Version:         version,
		DefaultHeadless: cfg.Headless,
		Open: func(ctx context.Context) (cli.Services, func(), error) {
			app, err := bootstrap.Open(ctx, cfg, logger)
			if err != nil {
				return cli.Services{}, nil, err
			}
			return cli.Services{Jobs: app.Jobs, Recipes: app.Recipes}, func() { _ = app.Close() }, nil
		},
	})
	err = root.ExecuteContext(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
	}
	return cli.ExitCode(err)
}
