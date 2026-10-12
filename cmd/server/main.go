// Comando server: API HTTP y cola de trabajos. Solo cablea dependencias.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"adaptive-scraper/internal/api"
	"adaptive-scraper/internal/application"
	"adaptive-scraper/internal/bootstrap"
	"adaptive-scraper/internal/config"
)

const (
	queueCapacity   = 1000
	shutdownTimeout = 10 * time.Second
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("el servidor terminó con error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	app, err := bootstrap.Open(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer app.Close()

	queue := application.NewQueue(app.Jobs, cfg.MaxConcurrentJobs, queueCapacity)
	pending, err := app.Jobs.RecoverInterrupted(ctx)
	if err != nil {
		return err
	}
	for _, id := range pending {
		if err := queue.Submit(id); err != nil {
			logger.Warn("trabajo pendiente sin sitio en la cola", "job_id", id, "error", err)
		}
	}
	queue.Start(ctx)

	handler := (&api.Server{
		Jobs: app.Jobs, Recipes: app.Recipes, Queue: queue, Logger: logger, DefaultHeadless: cfg.Headless,
	}).Handler()
	// Sin WriteTimeout: las respuestas SSE duran lo que dura el trabajo.
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}

	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ListenAndServe() }()
	logger.Info("servidor iniciado", "addr", cfg.HTTPAddr, "database", cfg.DatabasePath,
		"max_concurrent_jobs", cfg.MaxConcurrentJobs, "requeued", len(pending))

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}
	logger.Info("apagando")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	// Las conexiones SSE no terminan solas; al vencer el plazo se cierran.
	if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	_ = server.Close()
	queue.Wait()
	return nil
}
