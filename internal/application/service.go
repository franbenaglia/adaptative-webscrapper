package application

import (
	"context"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"time"

	"adaptive-scraper/internal/domain"
)

// Service implementa ScrapingService sobre un BrowserLauncher.
type Service struct {
	launcher          BrowserLauncher
	navigationTimeout time.Duration
	actionTimeout     time.Duration
}

var _ ScrapingService = (*Service)(nil)

// NewService crea el motor de extracción.
func NewService(launcher BrowserLauncher) *Service {
	return &Service{
		launcher:          launcher,
		navigationTimeout: 30 * time.Second,
		actionTimeout:     10 * time.Second,
	}
}

// Run ejecuta la receta de forma determinista hasta agotar páginas, registros
// o la paginación. Si falla a mitad de camino devuelve lo extraído hasta ese
// momento junto con el error.
func (s *Service) Run(ctx context.Context, request domain.ScrapeRequest, sink ProgressSink) (result domain.ScrapeResult, err error) {
	if sink == nil {
		sink = ProgressFunc(func(Event) {})
	}
	request, err = request.Normalize()
	if err != nil {
		return result, err
	}
	recipe := *request.Recipe
	result.Fields = recipe.Fields

	session, err := s.launcher.Open(ctx, BrowserOptions{
		Headless:          request.Headless,
		NavigationTimeout: s.navigationTimeout,
		ActionTimeout:     s.actionTimeout,
	})
	if err != nil {
		return result, fmt.Errorf("abrir navegador: %w", err)
	}
	defer func() {
		if cerr := session.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("cerrar navegador: %w", cerr)
		}
	}()

	if err := session.Navigate(ctx, request.URL); err != nil {
		return result, fmt.Errorf("navegar a %s: %w", request.URL, err)
	}

	missing := map[string]int{}
	seen := map[string]bool{}
	duplicates := 0
	var previous []domain.RawRecord

	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		pageURL := session.URL()
		if err := checkHost(recipe, pageURL); err != nil {
			return result, err
		}
		result.PagesVisited++
		sink.Emit(Event{Type: domain.EventPageOpened, Page: result.PagesVisited, URL: pageURL, Records: len(result.Records)})

		raws, err := session.Extract(ctx, recipe)
		if err != nil {
			return result, fmt.Errorf("extraer página %d: %w", result.PagesVisited, err)
		}
		// Un "siguiente" que no cambia el contenido repetiría la página sin fin.
		if result.PagesVisited > 1 && len(raws) > 0 && reflect.DeepEqual(raws, previous) {
			result.PagesVisited--
			result.Warnings = append(result.Warnings, "la paginación no cambió el contenido; se detuvo el recorrido")
			break
		}
		previous = raws
		if len(raws) == 0 {
			result.Warnings = append(result.Warnings, fmt.Sprintf("página %d: ningún elemento coincide con %q", result.PagesVisited, recipe.ItemLocator))
		}

		added := len(result.Records)
		for _, raw := range raws {
			if len(result.Records) >= request.MaxRecords {
				break
			}
			record, miss := domain.BuildRecord(recipe.Fields, raw, pageURL)
			for _, name := range miss {
				missing[name]++
			}
			key := fmt.Sprint(record)
			if seen[key] {
				duplicates++
			}
			seen[key] = true
			result.Records = append(result.Records, record)
		}
		sink.Emit(Event{Type: domain.EventRecordsExtracted, Page: result.PagesVisited, URL: pageURL,
			Records: len(result.Records), NewRecords: result.Records[added:]})

		if len(result.Records) >= request.MaxRecords || result.PagesVisited >= request.MaxPages || !recipe.HasPagination() {
			break
		}
		more, err := session.GoToNextPage(ctx, recipe.Pagination)
		if err != nil {
			return result, fmt.Errorf("pasar a la página %d: %w", result.PagesVisited+1, err)
		}
		if !more {
			break
		}
	}

	for _, f := range recipe.Fields {
		if n := missing[f.Name]; n > 0 {
			result.Warnings = append(result.Warnings, fmt.Sprintf("campo requerido %q ausente en %d de %d registros", f.Name, n, len(result.Records)))
		}
	}
	if duplicates > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("%d registros duplicados", duplicates))
	}
	return result, nil
}

func checkHost(recipe domain.Recipe, pageURL string) error {
	u, err := url.Parse(pageURL)
	if err != nil || !recipe.AllowsHost(u.Hostname()) || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("la navegación salió de los hosts permitidos (%s): %s",
			strings.Join(allowedHosts(recipe), ", "), pageURL)
	}
	return nil
}

func allowedHosts(recipe domain.Recipe) []string {
	if len(recipe.AllowedHosts) > 0 {
		return recipe.AllowedHosts
	}
	if u, err := url.Parse(recipe.StartURL); err == nil {
		return []string{u.Hostname()}
	}
	return nil
}
