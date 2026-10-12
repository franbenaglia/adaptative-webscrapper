package application

import (
	"context"
	"errors"
	"sync"
)

// ErrQueueFull indica que la cola en memoria no admite más trabajos.
var ErrQueueFull = errors.New("la cola de trabajos está llena")

// JobQueue acepta trabajos ya creados para ejecutarlos más tarde. La
// implementación actual vive en el proceso; el contrato permite sustituirla.
type JobQueue interface {
	Submit(jobID string) error
}

// Queue ejecuta trabajos con un número fijo de workers. Cada worker lanza
// como mucho un navegador a la vez.
type Queue struct {
	jobs    *JobService
	pending chan string
	workers int
	wg      sync.WaitGroup
}

var _ JobQueue = (*Queue)(nil)

func NewQueue(jobs *JobService, workers, capacity int) *Queue {
	return &Queue{jobs: jobs, pending: make(chan string, capacity), workers: max(workers, 1)}
}

// Start lanza los workers. Terminan cuando ctx se cancela; los trabajos en
// curso se cancelan y los pendientes siguen en estado queued en el repositorio.
func (q *Queue) Start(ctx context.Context) {
	for range q.workers {
		q.wg.Add(1)
		go func() {
			defer q.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case id := <-q.pending:
					// El resultado queda guardado en el trabajo; un trabajo que ya
					// no está en cola (cancelado mientras esperaba) se ignora.
					_, _ = q.jobs.Execute(ctx, id, nil)
				}
			}
		}()
	}
}

func (q *Queue) Submit(jobID string) error {
	select {
	case q.pending <- jobID:
		return nil
	default:
		return ErrQueueFull
	}
}

// Wait bloquea hasta que terminan todos los workers.
func (q *Queue) Wait() { q.wg.Wait() }
