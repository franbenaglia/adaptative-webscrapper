// Package application contiene los casos de uso. Es el único punto de entrada
// al motor de scraping, compartido por la CLI y la API HTTP.
package application

import (
	"context"
	"time"

	"adaptive-scraper/internal/domain"
)

// ScrapingService es el contrato que usan la CLI y la API.
type ScrapingService interface {
	Run(ctx context.Context, request domain.ScrapeRequest) (domain.ScrapeResult, error)
}

// BrowserOptions configura una sesión de navegador.
type BrowserOptions struct {
	Headless          bool
	NavigationTimeout time.Duration
	ActionTimeout     time.Duration
}

// BrowserLauncher abre sesiones aisladas, una por trabajo.
type BrowserLauncher interface {
	Open(ctx context.Context, options BrowserOptions) (BrowserSession, error)
}

// BrowserSession es una página dentro de un contexto de navegador propio.
type BrowserSession interface {
	Navigate(ctx context.Context, rawURL string) error
	// URL devuelve la URL actual, tras redirecciones o paginación.
	URL() string
	// Extract devuelve un RawRecord por cada elemento que cumple recipe.ItemLocator.
	Extract(ctx context.Context, recipe domain.Recipe) ([]domain.RawRecord, error)
	// GoToNextPage devuelve false cuando no hay página siguiente.
	GoToNextPage(ctx context.Context, rule domain.PaginationRule) (bool, error)
	Close() error
}

// Tipos de evento de progreso. Coinciden con los nombres que usará SSE.
const (
	EventPageOpened       = "page.opened"
	EventRecordsExtracted = "records.extracted"
)

// Event es una notificación de progreso de una ejecución.
type Event struct {
	Type    string
	Page    int
	URL     string
	Records int // total acumulado
}

// ProgressSink recibe eventos de progreso. Las implementaciones no deben bloquear.
type ProgressSink interface {
	Emit(event Event)
}

// ProgressFunc adapta una función a ProgressSink.
type ProgressFunc func(Event)

func (f ProgressFunc) Emit(event Event) { f(event) }
