package domain

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrNotFound          = errors.New("no encontrado")
	ErrInvalidTransition = errors.New("transición de estado no permitida")
)

// JobStatus es el estado de un trabajo.
type JobStatus string

const (
	JobQueued    JobStatus = "queued"
	JobRunning   JobStatus = "running"
	JobCompleted JobStatus = "completed"
	JobFailed    JobStatus = "failed"
	JobBlocked   JobStatus = "blocked"
	JobCancelled JobStatus = "cancelled"
)

// transitions enumera todas las transiciones válidas; los estados que no
// aparecen como clave son finales.
var transitions = map[JobStatus][]JobStatus{
	JobQueued:  {JobRunning, JobCancelled, JobFailed},
	JobRunning: {JobCompleted, JobFailed, JobBlocked, JobCancelled},
}

func (s JobStatus) Valid() bool {
	switch s {
	case JobQueued, JobRunning, JobCompleted, JobFailed, JobBlocked, JobCancelled:
		return true
	}
	return false
}

// Terminal indica que el trabajo ya no cambiará de estado.
func (s JobStatus) Terminal() bool {
	return s.Valid() && len(transitions[s]) == 0
}

func (s JobStatus) CanTransitionTo(to JobStatus) bool {
	for _, allowed := range transitions[s] {
		if allowed == to {
			return true
		}
	}
	return false
}

// Job es una ejecución registrada. Recipe es una copia de la receta usada,
// para que el historial no dependa de cambios posteriores; está vacía
// mientras un trabajo con Instruction no haya descubierto la suya.
type Job struct {
	ID           string     `json:"id"`
	RecipeID     string     `json:"recipe_id,omitempty"`
	Instruction  string     `json:"instruction,omitempty"`
	Recipe       Recipe     `json:"recipe"`
	RecipeOrigin string     `json:"recipe_origin,omitempty"`
	URL          string     `json:"url"`
	Status       JobStatus  `json:"status"`
	MaxPages     int        `json:"max_pages"`
	MaxRecords   int        `json:"max_records"`
	Headless     bool       `json:"headless"`
	PagesVisited int        `json:"pages_visited"`
	RecordCount  int        `json:"record_count"`
	Warnings     []string   `json:"warnings"`
	Error        string     `json:"error,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
}

// Transition cambia el estado si la transición es válida y anota la hora.
func (j *Job) Transition(to JobStatus, now time.Time) error {
	if !j.Status.CanTransitionTo(to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, j.Status, to)
	}
	j.Status = to
	if to == JobRunning {
		j.StartedAt = &now
	}
	if to.Terminal() {
		j.FinishedAt = &now
	}
	return nil
}

// Request reconstruye la solicitud de extracción del trabajo.
func (j Job) Request() ScrapeRequest {
	request := ScrapeRequest{URL: j.URL, Instruction: j.Instruction, MaxPages: j.MaxPages, MaxRecords: j.MaxRecords, Headless: j.Headless}
	if !j.Recipe.IsZero() {
		recipe := j.Recipe
		request.Recipe = &recipe
	}
	return request
}

// Tipos de evento de un trabajo; son también los nombres de evento de SSE.
const (
	EventJobStarted       = "job.started"
	EventPageOpened       = "page.opened"
	EventRecordsExtracted = "records.extracted"
	EventRecipeDiscovered = "recipe.discovered"
	EventRecipeValidated  = "recipe.validated"
	EventRecipeRepairing  = "recipe.repairing"
	EventJobCompleted     = "job.completed"
	EventJobFailed        = "job.failed"
	EventJobBlocked       = "job.blocked"
	EventJobCancelled     = "job.cancelled"
)

// JobEvent es un hito de un trabajo. Seq es creciente dentro del trabajo.
type JobEvent struct {
	JobID   string    `json:"job_id"`
	Seq     int64     `json:"seq"`
	Type    string    `json:"type"`
	At      time.Time `json:"at"`
	Page    int       `json:"page,omitempty"`
	URL     string    `json:"url,omitempty"`
	Records int       `json:"records"`
	Message string    `json:"message,omitempty"`
}

// Terminal indica que no habrá más eventos del trabajo.
func (e JobEvent) Terminal() bool {
	switch e.Type {
	case EventJobCompleted, EventJobFailed, EventJobBlocked, EventJobCancelled:
		return true
	}
	return false
}

// TerminalEventType devuelve el tipo de evento que corresponde a un estado final.
func TerminalEventType(status JobStatus) string {
	return "job." + string(status)
}
