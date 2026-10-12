// Package cli define los comandos de Cobra. No contiene lógica de scraping:
// traduce flags a solicitudes y delega en los servicios de application, los
// mismos que usa la API HTTP.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

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

// Services son los casos de uso que necesitan los comandos.
type Services struct {
	Jobs     *application.JobService
	Recipes  *application.RecipeService
	AIStatus func(ctx context.Context) application.AIStatus
}

// Deps son las dependencias de los comandos. Open se invoca solo cuando un
// comando necesita los servicios y devuelve además la función que los cierra.
type Deps struct {
	Open            func(ctx context.Context) (Services, func(), error)
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
	root.AddCommand(newScrapeCommand(deps), newRecipesCommand(deps), newJobsCommand(deps), newAICommand(deps), newVersionCommand(deps))
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

// runFlags son los flags comunes a `scrape` y `recipes run`.
type runFlags struct {
	url, output, format  string
	maxPages, maxRecords int
	headless             bool
	saveRecipe           bool
}

func (f *runFlags) register(cmd *cobra.Command, deps Deps) {
	flags := cmd.Flags()
	flags.StringVar(&f.url, "url", "", "URL inicial (por defecto, start_url de la receta)")
	flags.IntVar(&f.maxPages, "max-pages", domain.DefaultMaxPages, "máximo de páginas a recorrer")
	flags.IntVar(&f.maxRecords, "max-records", domain.DefaultMaxRecords, "máximo de registros a extraer")
	flags.StringVarP(&f.output, "output", "o", "", "archivo de salida (por defecto, stdout)")
	flags.StringVar(&f.format, "format", "", "json o csv (por defecto, según la extensión de --output)")
	flags.BoolVar(&f.headless, "headless", deps.DefaultHeadless, "ejecutar el navegador sin ventana")
}

func newScrapeCommand(deps Deps) *cobra.Command {
	var (
		flags       runFlags
		recipePath  string
		recipeID    string
		instruction string
	)
	cmd := &cobra.Command{
		Use:   "scrape",
		Short: "Extrae registros con una receta o, con IA, a partir de una instrucción",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			request := application.JobRequest{RecipeID: recipeID, Instruction: instruction}
			switch {
			case recipePath != "" && recipeID != "":
				return errors.New("use --recipe o --recipe-id, no ambos")
			case recipePath != "":
				recipe, err := LoadRecipe(recipePath)
				if err != nil {
					return err
				}
				request.Recipe = &recipe
			case recipeID == "" && (instruction == "" || flags.url == ""):
				return errors.New("indique --recipe o --recipe-id, o bien --url e --instruction para descubrir la receta con IA")
			}
			return runJob(cmd, deps, flags, request)
		},
	}
	flags.register(cmd, deps)
	cmd.Flags().StringVar(&recipePath, "recipe", "", "archivo JSON con la receta")
	cmd.Flags().StringVar(&recipeID, "recipe-id", "", "identificador de una receta guardada")
	cmd.Flags().StringVar(&instruction, "instruction", "", "qué datos extraer, en lenguaje natural (requiere IA)")
	cmd.Flags().BoolVar(&flags.saveRecipe, "save-recipe", false, "guardar la receta descubierta para reutilizarla sin IA")
	return cmd
}

func newAICommand(deps Deps) *cobra.Command {
	cmd := &cobra.Command{Use: "ai", Short: "Consulta el proveedor de IA"}
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Muestra el modelo configurado y si está disponible",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			services, closeServices, err := deps.Open(cmd.Context())
			if err != nil {
				return err
			}
			defer closeServices()
			return printJSON(cmd.OutOrStdout(), services.AIStatus(cmd.Context()))
		},
	})
	return cmd
}

// runJob crea el trabajo, lo ejecuta en este proceso y exporta sus registros.
func runJob(cmd *cobra.Command, deps Deps, flags runFlags, request application.JobRequest) error {
	format := flags.format
	if format == "" {
		format = export.FormatFromPath(flags.output)
	}
	exporter, err := export.ForFormat(format)
	if err != nil {
		return err
	}
	services, closeServices, err := deps.Open(cmd.Context())
	if err != nil {
		return err
	}
	defer closeServices()

	request.URL, request.MaxPages, request.MaxRecords, request.Headless = flags.url, flags.maxPages, flags.maxRecords, flags.headless
	job, err := services.Jobs.Create(cmd.Context(), request)
	if err != nil {
		return err
	}
	stderr := cmd.ErrOrStderr()
	fmt.Fprintf(stderr, "trabajo %s\n", job.ID)
	job, runErr := services.Jobs.Execute(cmd.Context(), job.ID, func(e domain.JobEvent) {
		switch e.Type {
		case domain.EventPageOpened:
			fmt.Fprintf(stderr, "página %d: %s\n", e.Page, e.URL)
		case domain.EventRecordsExtracted:
			fmt.Fprintf(stderr, "página %d: %d registros acumulados\n", e.Page, e.Records)
		case domain.EventRecipeDiscovered:
			fmt.Fprintf(stderr, "receta descubierta por el modelo (%s)\n", e.Message)
		case domain.EventRecipeRepairing:
			fmt.Fprintf(stderr, "reparando la receta con el modelo: %s\n", e.Message)
		case domain.EventRecipeValidated:
			fmt.Fprintf(stderr, "receta validada: %s\n", e.Message)
		}
	})
	if flags.saveRecipe && job.RecipeID == "" && !job.Recipe.IsZero() {
		// Se guarda aunque el contexto del comando esté cancelado.
		if saved, err := services.Recipes.Create(context.WithoutCancel(cmd.Context()), job.Recipe); err != nil {
			fmt.Fprintln(stderr, "aviso: no se pudo guardar la receta:", err)
		} else {
			fmt.Fprintf(stderr, "receta guardada: %s\n", saved.ID)
		}
	}
	for _, w := range job.Warnings {
		fmt.Fprintln(stderr, "aviso:", w)
	}
	// Ante un fallo tardío se exporta igualmente lo ya extraído. La lectura
	// no usa el contexto del comando porque puede estar cancelado.
	if runErr != nil && job.RecordCount == 0 {
		return runErr
	}
	records, err := services.Jobs.Records(context.WithoutCancel(cmd.Context()), job.ID, 0, 0)
	if err != nil {
		return errors.Join(runErr, err)
	}
	if err := write(cmd.OutOrStdout(), flags.output, exporter, job.Recipe.Fields, records); err != nil {
		return errors.Join(runErr, err)
	}
	fmt.Fprintf(stderr, "%d registros en %d páginas (%s)\n", len(records), job.PagesVisited, job.Status)
	return runErr
}

func newRecipesCommand(deps Deps) *cobra.Command {
	cmd := &cobra.Command{Use: "recipes", Short: "Gestiona las recetas guardadas"}
	var asJSON bool

	list := &cobra.Command{
		Use:   "list",
		Short: "Lista las recetas guardadas",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			services, closeServices, err := deps.Open(cmd.Context())
			if err != nil {
				return err
			}
			defer closeServices()
			recipes, err := services.Recipes.List(cmd.Context())
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(cmd.OutOrStdout(), recipes)
			}
			return table(cmd.OutOrStdout(), "ID\tNOMBRE\tVERSIÓN\tCAMPOS\tURL", len(recipes), func(i int) string {
				r := recipes[i]
				return fmt.Sprintf("%s\t%s\t%d\t%d\t%s", r.ID, r.Name, r.Version, len(r.Fields), r.StartURL)
			})
		},
	}
	list.Flags().BoolVar(&asJSON, "json", false, "salida en JSON")

	show := &cobra.Command{
		Use:   "show <id>",
		Short: "Muestra una receta en JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			services, closeServices, err := deps.Open(cmd.Context())
			if err != nil {
				return err
			}
			defer closeServices()
			recipe, err := services.Recipes.Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), recipe)
		},
	}

	importCmd := &cobra.Command{
		Use:   "import <archivo.json>",
		Short: "Guarda una receta desde un archivo JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			recipe, err := LoadRecipe(args[0])
			if err != nil {
				return err
			}
			services, closeServices, err := deps.Open(cmd.Context())
			if err != nil {
				return err
			}
			defer closeServices()
			saved, err := services.Recipes.Create(cmd.Context(), recipe)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), saved.ID)
			return nil
		},
	}

	var flags runFlags
	run := &cobra.Command{
		Use:   "run <id>",
		Short: "Ejecuta una receta guardada",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJob(cmd, deps, flags, application.JobRequest{RecipeID: args[0]})
		},
	}
	flags.register(run, deps)

	cmd.AddCommand(list, show, importCmd, run)
	return cmd
}

func newJobsCommand(deps Deps) *cobra.Command {
	cmd := &cobra.Command{Use: "jobs", Short: "Consulta el historial de trabajos"}
	var (
		asJSON bool
		limit  int
	)
	list := &cobra.Command{
		Use:   "list",
		Short: "Lista los trabajos, del más reciente al más antiguo",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			services, closeServices, err := deps.Open(cmd.Context())
			if err != nil {
				return err
			}
			defer closeServices()
			jobs, err := services.Jobs.List(cmd.Context(), application.JobFilter{Limit: limit})
			if err != nil {
				return err
			}
			if asJSON {
				return printJSON(cmd.OutOrStdout(), jobs)
			}
			return table(cmd.OutOrStdout(), "ID\tESTADO\tREGISTROS\tPÁGINAS\tCREADO\tRECETA", len(jobs), func(i int) string {
				j := jobs[i]
				return fmt.Sprintf("%s\t%s\t%d\t%d\t%s\t%s", j.ID, j.Status, j.RecordCount, j.PagesVisited,
					j.CreatedAt.Local().Format(time.DateTime), j.Recipe.Name)
			})
		},
	}
	list.Flags().BoolVar(&asJSON, "json", false, "salida en JSON")
	list.Flags().IntVar(&limit, "limit", 20, "máximo de trabajos (0 = todos)")

	show := &cobra.Command{
		Use:   "show <id>",
		Short: "Muestra un trabajo en JSON, con sus eventos",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			services, closeServices, err := deps.Open(cmd.Context())
			if err != nil {
				return err
			}
			defer closeServices()
			job, err := services.Jobs.Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			events, err := services.Jobs.Events(cmd.Context(), job.ID, 0)
			if err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), struct {
				domain.Job
				Events []domain.JobEvent `json:"events"`
			}{job, events})
		},
	}

	cmd.AddCommand(list, show)
	return cmd
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func table(w io.Writer, header string, rows int, row func(int) string) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, header)
	for i := range rows {
		fmt.Fprintln(tw, row(i))
	}
	return tw.Flush()
}

func write(stdout io.Writer, output string, exporter export.Exporter, fields []domain.FieldDefinition, records []domain.Record) (err error) {
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
	return exporter.Write(w, fields, records)
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
