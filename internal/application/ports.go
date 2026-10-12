// Package application contiene los casos de uso. Es el único punto de entrada
// al motor de scraping, compartido por la CLI y la API HTTP.
package application

import (
	"context"
	"time"

	"adaptive-scraper/internal/domain"
)

// ScrapingService es el motor de extracción: ejecuta una receta de principio
// a fin. sink puede ser nil.
type ScrapingService interface {
	Run(ctx context.Context, request domain.ScrapeRequest, sink ProgressSink) (domain.ScrapeResult, error)
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

// Event es una notificación de progreso del motor. Type es uno de
// domain.EventPageOpened o domain.EventRecordsExtracted.
type Event struct {
	Type    string
	Page    int
	URL     string
	Records int // total acumulado
	// NewRecords son los registros añadidos en esta página (solo en records.extracted).
	NewRecords []domain.Record
}

// ProgressSink recibe eventos de progreso de forma síncrona, en orden.
type ProgressSink interface {
	Emit(event Event)
}

// ProgressFunc adapta una función a ProgressSink.
type ProgressFunc func(Event)

func (f ProgressFunc) Emit(event Event) { f(event) }

// JobFilter acota un listado de trabajos. Limit <= 0 significa sin límite.
type JobFilter struct {
	Status domain.JobStatus
	Limit  int
}

// RecipeRepository guarda recetas y sus versiones.
type RecipeRepository interface {
	Create(ctx context.Context, recipe domain.Recipe, now time.Time) error
	Update(ctx context.Context, recipe domain.Recipe, now time.Time) error
	Get(ctx context.Context, id string) (domain.Recipe, error)
	List(ctx context.Context) ([]domain.Recipe, error)
	Delete(ctx context.Context, id string) error
}

// JobRepository guarda trabajos junto con sus eventos y registros.
type JobRepository interface {
	Create(ctx context.Context, job domain.Job) error
	Update(ctx context.Context, job domain.Job) error
	Get(ctx context.Context, id string) (domain.Job, error)
	List(ctx context.Context, filter JobFilter) ([]domain.Job, error)
	AppendRecords(ctx context.Context, jobID string, start int, records []domain.Record) error
	Records(ctx context.Context, jobID string, offset, limit int) ([]domain.Record, error)
	AppendEvent(ctx context.Context, event domain.JobEvent) (domain.JobEvent, error)
	Events(ctx context.Context, jobID string, after int64) ([]domain.JobEvent, error)
}
