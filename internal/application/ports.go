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
	// Snapshot resume la página actual en candidatos para el modelo.
	Snapshot(ctx context.Context) (domain.PageSnapshot, error)
	// BlockReason describe por qué la página impide el acceso (login,
	// CAPTCHA, verificación humana) o devuelve "" si no lo impide.
	BlockReason(ctx context.Context) (string, error)
	Close() error
}

// IntelligenceProvider propone recetas a partir de un PageSnapshot. Su
// resultado es entrada no confiable: el motor lo valida y lo prueba.
type IntelligenceProvider interface {
	DiscoverRecipe(ctx context.Context, snapshot domain.PageSnapshot, instruction string) (domain.Recipe, error)
	// RepairRecipe debe conservar los nombres y tipos de campo de previous.
	RepairRecipe(ctx context.Context, previous domain.Recipe, snapshot domain.PageSnapshot, failure domain.ExtractionFailure) (domain.Recipe, error)
}

// AIStatus describe el estado del proveedor de IA para mostrarlo al usuario.
type AIStatus struct {
	Enabled         bool     `json:"enabled"`
	Provider        string   `json:"provider,omitempty"`
	Model           string   `json:"model,omitempty"`
	Reachable       bool     `json:"reachable"`
	ModelInstalled  bool     `json:"model_installed"`
	InstalledModels []string `json:"installed_models"`
	Message         string   `json:"message,omitempty"`
}

// Event es una notificación de progreso del motor. Type es uno de los
// domain.Event* de página, registros o receta.
type Event struct {
	Type    string
	Page    int
	URL     string
	Records int // total acumulado
	Message string
	// NewRecords son los registros añadidos en esta página (solo en records.extracted).
	NewRecords []domain.Record
	// Recipe y RecipeOrigin acompañan a recipe.discovered y recipe.validated.
	Recipe       *domain.Recipe
	RecipeOrigin string
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
