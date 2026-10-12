package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"adaptive-scraper/internal/domain"
)

// JobRequest es lo que piden la CLI y la API para crear un trabajo. Puede
// indicar RecipeID (receta guardada) o Recipe (receta en línea), no ambos.
// Sin receta, URL e Instruction piden descubrirla con IA.
type JobRequest struct {
	URL         string
	Instruction string
	RecipeID    string
	Recipe      *domain.Recipe
	MaxPages    int
	MaxRecords  int
	Headless    bool
}

// JobOptions ajusta un JobService. Los valores cero usan valores por defecto.
type JobOptions struct {
	Logger *slog.Logger
	// Timeout es la duración máxima de un trabajo; 0 significa sin límite.
	Timeout time.Duration
	// AIEnabled permite crear trabajos sin receta.
	AIEnabled bool
	Now       func() time.Time
	NewID     func() string
}

// JobService es el caso de uso compartido por la CLI y la API: crea trabajos,
// los ejecuta con el motor y deja constancia de estado, eventos y registros.
type JobService struct {
	engine  ScrapingService
	jobs    JobRepository
	recipes RecipeRepository
	hub     *EventHub
	logger  *slog.Logger
	timeout time.Duration
	ai      bool
	now     func() time.Time
	newID   func() string

	// mu serializa los cambios de estado que parten de leer el trabajo.
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

func NewJobService(engine ScrapingService, jobs JobRepository, recipes RecipeRepository, options JobOptions) *JobService {
	if options.Logger == nil {
		options.Logger = slog.New(slog.DiscardHandler)
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.NewID == nil {
		options.NewID = NewID
	}
	return &JobService{
		engine: engine, jobs: jobs, recipes: recipes, hub: NewEventHub(),
		logger: options.Logger, timeout: options.Timeout, ai: options.AIEnabled, now: options.Now, newID: options.NewID,
		cancels: map[string]context.CancelFunc{},
	}
}

// Create valida la solicitud y guarda el trabajo en estado queued. Es el
// único lugar donde se validan solicitudes, sea cual sea su origen.
func (s *JobService) Create(ctx context.Context, request JobRequest) (domain.Job, error) {
	var recipe *domain.Recipe
	switch {
	case request.RecipeID != "" && request.Recipe != nil:
		return domain.Job{}, fmt.Errorf("%w: indique recipe_id o recipe, no ambos", domain.ErrInvalidRequest)
	case request.RecipeID != "":
		stored, err := s.recipes.Get(ctx, request.RecipeID)
		if err != nil {
			return domain.Job{}, err
		}
		recipe = &stored
	case request.Recipe != nil:
		recipe = request.Recipe
	case !s.ai:
		return domain.Job{}, fmt.Errorf("%w: falta la receta; para descubrirla a partir de una instrucción active la IA (AI_ENABLED=true)", domain.ErrInvalidRequest)
	}

	normalized, err := domain.ScrapeRequest{
		URL: request.URL, Instruction: request.Instruction, Recipe: recipe,
		MaxPages: request.MaxPages, MaxRecords: request.MaxRecords, Headless: request.Headless,
	}.Normalize()
	if err != nil {
		return domain.Job{}, err
	}
	job := domain.Job{
		ID: s.newID(), RecipeID: request.RecipeID, Instruction: normalized.Instruction, URL: normalized.URL, Status: domain.JobQueued,
		MaxPages: normalized.MaxPages, MaxRecords: normalized.MaxRecords, Headless: normalized.Headless,
		Warnings: []string{}, CreatedAt: s.now(),
	}
	if recipe != nil {
		job.Recipe, job.RecipeOrigin = *recipe, domain.RecipeProvided
	} else {
		job.Recipe.Fields = []domain.FieldDefinition{}
	}
	return job, s.jobs.Create(ctx, job)
}

// Execute ejecuta un trabajo en estado queued hasta un estado final y lo
// devuelve. El error describe por qué no terminó como completed; es
// context.Canceled si se canceló. onEvent, si no es nil, recibe cada evento.
//
// Los registros y eventos se guardan a medida que se producen, y el estado
// final se guarda aunque ctx ya esté cancelado.
func (s *JobService) Execute(ctx context.Context, id string, onEvent func(domain.JobEvent)) (domain.Job, error) {
	persist := context.WithoutCancel(ctx)
	runCtx, cancel := context.WithCancel(ctx)
	if s.timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, s.timeout)
	}
	defer cancel()

	s.mu.Lock()
	job, err := s.jobs.Get(persist, id)
	if err == nil {
		err = job.Transition(domain.JobRunning, s.now())
	}
	if err == nil {
		err = s.jobs.Update(persist, job)
	}
	if err == nil {
		s.cancels[id] = cancel
	}
	s.mu.Unlock()
	if err != nil {
		return job, err
	}
	defer func() {
		s.mu.Lock()
		delete(s.cancels, id)
		s.mu.Unlock()
	}()

	emit := func(event domain.JobEvent) {
		saved := s.emit(persist, id, event)
		if onEvent != nil {
			onEvent(saved)
		}
	}
	emit(domain.JobEvent{Type: domain.EventJobStarted, URL: job.URL})

	var persistErr error
	sink := ProgressFunc(func(e Event) {
		if persistErr != nil {
			return
		}
		if len(e.NewRecords) > 0 {
			if persistErr = s.jobs.AppendRecords(persist, id, job.RecordCount, e.NewRecords); persistErr != nil {
				cancel()
				return
			}
			job.RecordCount += len(e.NewRecords)
		}
		job.PagesVisited = e.Page
		if e.Recipe != nil {
			job.Recipe, job.RecipeOrigin = *e.Recipe, e.RecipeOrigin
		}
		if persistErr = s.jobs.Update(persist, job); persistErr != nil {
			cancel()
			return
		}
		emit(domain.JobEvent{Type: e.Type, Page: e.Page, URL: e.URL, Records: job.RecordCount, Message: e.Message})
	})

	result, runErr := s.run(runCtx, job.Request(), sink)
	if persistErr != nil {
		runErr = fmt.Errorf("guardar el progreso: %w", persistErr)
	}

	status := domain.JobCompleted
	switch {
	case runErr == nil:
	case persistErr == nil && errors.Is(runErr, context.DeadlineExceeded) && ctx.Err() == nil:
		status, runErr = domain.JobFailed, fmt.Errorf("se superó el tiempo máximo del trabajo (%s)", s.timeout)
	case persistErr == nil && errors.Is(runErr, context.Canceled):
		status, runErr = domain.JobCancelled, fmt.Errorf("trabajo cancelado: %w", context.Canceled)
	case errors.Is(runErr, domain.ErrBlocked):
		status = domain.JobBlocked
	default:
		status = domain.JobFailed
	}

	job.PagesVisited = max(job.PagesVisited, result.PagesVisited)
	job.Warnings = append([]string{}, result.Warnings...)
	if result.Recipe != nil {
		job.Recipe, job.RecipeOrigin = *result.Recipe, result.RecipeOrigin
	}
	if status == domain.JobCompleted && job.RecipeOrigin == domain.RecipeRepaired && job.RecipeID != "" {
		job.Warnings = append(job.Warnings, s.saveRepaired(persist, &job))
	}
	if runErr != nil {
		job.Error = runErr.Error()
	}
	if err := job.Transition(status, s.now()); err != nil {
		return job, err
	}
	if err := s.jobs.Update(persist, job); err != nil {
		return job, errors.Join(runErr, fmt.Errorf("guardar el estado final: %w", err))
	}
	emit(domain.JobEvent{Type: domain.TerminalEventType(status), Records: job.RecordCount, Message: job.Error})
	return job, runErr
}

// saveRepaired guarda la receta reparada como nueva versión de la receta
// guardada de la que partió el trabajo, y devuelve el aviso para el usuario.
func (s *JobService) saveRepaired(ctx context.Context, job *domain.Job) string {
	current, err := s.recipes.Get(ctx, job.RecipeID)
	if err == nil {
		job.Recipe.ID, job.Recipe.Version = current.ID, current.Version+1
		err = s.recipes.Update(ctx, job.Recipe, s.now())
	}
	if err != nil {
		s.logger.Error("no se pudo guardar la receta reparada", "job_id", job.ID, "recipe_id", job.RecipeID, "error", err)
		return fmt.Sprintf("la receta se reparó para este trabajo, pero no se pudo guardar: %v", err)
	}
	return fmt.Sprintf("la receta dejó de funcionar y se reparó; guardada como versión %d", job.Recipe.Version)
}

// run aísla al resto del proceso de un pánico en el motor o en un adaptador.
func (s *JobService) run(ctx context.Context, request domain.ScrapeRequest, sink ProgressSink) (result domain.ScrapeResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("pánico durante un trabajo", "panic", r, "stack", string(debug.Stack()))
			err = fmt.Errorf("error interno durante el trabajo: %v", r)
		}
	}()
	return s.engine.Run(ctx, request, sink)
}

func (s *JobService) emit(ctx context.Context, jobID string, event domain.JobEvent) domain.JobEvent {
	event.JobID, event.At = jobID, s.now()
	saved, err := s.jobs.AppendEvent(ctx, event)
	if err != nil {
		s.logger.Error("no se pudo guardar un evento", "job_id", jobID, "type", event.Type, "error", err)
		saved = event
	}
	s.logger.Info("evento de trabajo", "job_id", jobID, "type", saved.Type, "page", saved.Page, "records", saved.Records)
	s.hub.Publish(saved)
	return saved
}

// Cancel cancela un trabajo. Si está en cola pasa a cancelled de inmediato;
// si se está ejecutando en este proceso se interrumpe y pasará a cancelled
// en breve.
func (s *JobService) Cancel(ctx context.Context, id string) (domain.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, err := s.jobs.Get(ctx, id)
	if err != nil {
		return job, err
	}
	if job.Status == domain.JobRunning {
		cancel, ok := s.cancels[id]
		if !ok {
			return job, fmt.Errorf("%w: el trabajo se ejecuta en otro proceso", domain.ErrInvalidTransition)
		}
		cancel()
		return job, nil
	}
	return s.finishQueued(ctx, job, domain.JobCancelled, "cancelado antes de empezar")
}

// Reject marca como fallido un trabajo en cola que no podrá ejecutarse.
func (s *JobService) Reject(ctx context.Context, id, reason string) (domain.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, err := s.jobs.Get(ctx, id)
	if err != nil {
		return job, err
	}
	return s.finishQueued(ctx, job, domain.JobFailed, reason)
}

func (s *JobService) finishQueued(ctx context.Context, job domain.Job, status domain.JobStatus, reason string) (domain.Job, error) {
	if job.Status != domain.JobQueued {
		return job, fmt.Errorf("%w: el trabajo está en estado %s", domain.ErrInvalidTransition, job.Status)
	}
	if err := job.Transition(status, s.now()); err != nil {
		return job, err
	}
	job.Error = reason
	if err := s.jobs.Update(ctx, job); err != nil {
		return job, err
	}
	s.emit(ctx, job.ID, domain.JobEvent{Type: domain.TerminalEventType(status), Message: reason})
	return job, nil
}

// RecoverInterrupted repara el estado tras un reinicio: marca como fallidos
// los trabajos que quedaron en running y devuelve los que siguen en cola,
// del más antiguo al más reciente, para volver a encolarlos.
func (s *JobService) RecoverInterrupted(ctx context.Context) ([]string, error) {
	running, err := s.jobs.List(ctx, JobFilter{Status: domain.JobRunning})
	if err != nil {
		return nil, err
	}
	for _, job := range running {
		_ = job.Transition(domain.JobFailed, s.now())
		job.Error = "interrumpido por un reinicio del servidor"
		if err := s.jobs.Update(ctx, job); err != nil {
			return nil, err
		}
		s.emit(ctx, job.ID, domain.JobEvent{Type: domain.EventJobFailed, Records: job.RecordCount, Message: job.Error})
	}
	queued, err := s.jobs.List(ctx, JobFilter{Status: domain.JobQueued})
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(queued))
	for i, job := range queued {
		ids[len(queued)-1-i] = job.ID
	}
	return ids, nil
}

func (s *JobService) Get(ctx context.Context, id string) (domain.Job, error) {
	return s.jobs.Get(ctx, id)
}

func (s *JobService) List(ctx context.Context, filter JobFilter) ([]domain.Job, error) {
	return s.jobs.List(ctx, filter)
}

// Records devuelve registros del trabajo. limit <= 0 significa todos.
func (s *JobService) Records(ctx context.Context, id string, offset, limit int) ([]domain.Record, error) {
	return s.jobs.Records(ctx, id, offset, limit)
}

// Events devuelve los eventos guardados con Seq mayor que after.
func (s *JobService) Events(ctx context.Context, id string, after int64) ([]domain.JobEvent, error) {
	return s.jobs.Events(ctx, id, after)
}

// Subscribe sigue en vivo los eventos de un trabajo; ver EventHub.Subscribe.
func (s *JobService) Subscribe(id string) (<-chan domain.JobEvent, func()) {
	return s.hub.Subscribe(id)
}
