// Comando adaptive-scraper: solo cablea dependencias y delega en internal/cli.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"adaptive-scraper/internal/application"
	"adaptive-scraper/internal/browser"
	"adaptive-scraper/internal/cli"
)

// version se sobrescribe con -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	launcher := browser.NewLauncher()
	defer launcher.Close()

	progress := application.ProgressFunc(func(e application.Event) {
		switch e.Type {
		case application.EventPageOpened:
			fmt.Fprintf(os.Stderr, "página %d: %s\n", e.Page, e.URL)
		case application.EventRecordsExtracted:
			fmt.Fprintf(os.Stderr, "página %d: %d registros acumulados\n", e.Page, e.Records)
		}
	})

	root := cli.NewRootCommand(cli.Deps{
		Service:         application.NewService(launcher, progress),
		Version:         version,
		DefaultHeadless: envBool("PLAYWRIGHT_HEADLESS", true),
	})
	err := root.ExecuteContext(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
	}
	return cli.ExitCode(err)
}

func envBool(name string, fallback bool) bool {
	if v, err := strconv.ParseBool(os.Getenv(name)); err == nil {
		return v
	}
	return fallback
}
