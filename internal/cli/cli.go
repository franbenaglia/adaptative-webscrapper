// Package cli define los comandos de Cobra. No contiene lógica de scraping:
// traduce flags a un domain.ScrapeRequest y delega en application.ScrapingService.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"adaptive-scraper/internal/application"
	"adaptive-scraper/internal/domain"
	"adaptive-scraper/internal/export"
)

// Códigos de salida. El 2 queda reservado para errores de uso del shell.
const (
	ExitOK            = 0
	ExitError         = 1
	ExitBlocked       = 3
	ExitCancelled     = 4
	ExitInvalidRecipe = 5
)

// Deps son las dependencias de los comandos.
type Deps struct {
	Service         application.ScrapingService
	Version         string
	DefaultHeadless bool
}

// NewRootCommand construye el árbol de comandos.
func NewRootCommand(deps Deps) *cobra.Command {
	root := &cobra.Command{
		Use:           "adaptive-scraper",
		Short:         "Extracción web con recetas declarativas",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newScrapeCommand(deps), newVersionCommand(deps))
	return root
}

func newVersionCommand(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Muestra la versión",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), deps.Version)
		},
	}
}

func newScrapeCommand(deps Deps) *cobra.Command {
	var (
		rawURL, recipePath, output, format string
		maxPages, maxRecords               int
		headless                           bool
	)
	cmd := &cobra.Command{
		Use:   "scrape",
		Short: "Ejecuta una receta y exporta los registros",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if recipePath == "" {
				return errors.New("falta --recipe: el descubrimiento de recetas con IA aún no está disponible")
			}
			recipe, err := LoadRecipe(recipePath)
			if err != nil {
				return err
			}
			if format == "" {
				format = export.FormatFromPath(output)
			}
			exporter, err := export.ForFormat(format)
			if err != nil {
				return err
			}

			result, runErr := deps.Service.Run(cmd.Context(), domain.ScrapeRequest{
				URL:        rawURL,
				Recipe:     &recipe,
				MaxPages:   maxPages,
				MaxRecords: maxRecords,
				Headless:   headless,
			})
			stderr := cmd.ErrOrStderr()
			for _, w := range result.Warnings {
				fmt.Fprintln(stderr, "aviso:", w)
			}
			// Ante un fallo tardío se exporta igualmente lo ya extraído.
			if runErr != nil && len(result.Records) == 0 {
				return runErr
			}
			if err := write(cmd.OutOrStdout(), output, exporter, result); err != nil {
				return errors.Join(runErr, err)
			}
			fmt.Fprintf(stderr, "%d registros en %d páginas\n", len(result.Records), result.PagesVisited)
			return runErr
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&rawURL, "url", "", "URL inicial (por defecto, start_url de la receta)")
	flags.StringVar(&recipePath, "recipe", "", "archivo JSON con la receta")
	flags.IntVar(&maxPages, "max-pages", domain.DefaultMaxPages, "máximo de páginas a recorrer")
	flags.IntVar(&maxRecords, "max-records", domain.DefaultMaxRecords, "máximo de registros a extraer")
	flags.StringVarP(&output, "output", "o", "", "archivo de salida (por defecto, stdout)")
	flags.StringVar(&format, "format", "", "json o csv (por defecto, según la extensión de --output)")
	flags.BoolVar(&headless, "headless", deps.DefaultHeadless, "ejecutar el navegador sin ventana")
	return cmd
}

func write(stdout io.Writer, output string, exporter export.Exporter, result domain.ScrapeResult) (err error) {
	w := stdout
	if output != "" {
		f, err := os.Create(output)
		if err != nil {
			return fmt.Errorf("crear %s: %w", output, err)
		}
		defer func() {
			if cerr := f.Close(); err == nil {
				err = cerr
			}
		}()
		w = f
	}
	return exporter.Write(w, result.Fields, result.Records)
}

// LoadRecipe lee una receta de un archivo JSON con decodificación estricta.
func LoadRecipe(path string) (domain.Recipe, error) {
	var recipe domain.Recipe
	f, err := os.Open(path)
	if err != nil {
		return recipe, fmt.Errorf("%w: %v", domain.ErrInvalidRecipe, err)
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&recipe); err != nil {
		return recipe, fmt.Errorf("%w: %s: %v", domain.ErrInvalidRecipe, path, err)
	}
	if err := recipe.Validate(); err != nil {
		return recipe, fmt.Errorf("%s: %w", path, err)
	}
	return recipe, nil
}

// ExitCode traduce un error al código de salida del proceso.
func ExitCode(err error) int {
	switch {
	case err == nil:
		return ExitOK
	case errors.Is(err, context.Canceled):
		return ExitCancelled
	case errors.Is(err, domain.ErrBlocked):
		return ExitBlocked
	case errors.Is(err, domain.ErrInvalidRecipe):
		return ExitInvalidRecipe
	default:
		return ExitError
	}
}
