package application

import (
	"sync"

	"adaptive-scraper/internal/domain"
)

const subscriberBuffer = 64

// EventHub reparte los eventos de los trabajos en curso a quienes los siguen
// en vivo (SSE). No guarda historial: eso lo hace JobRepository.
type EventHub struct {
	mu   sync.Mutex
	subs map[string]map[chan domain.JobEvent]struct{}
}

func NewEventHub() *EventHub {
	return &EventHub{subs: map[string]map[chan domain.JobEvent]struct{}{}}
}

// Subscribe devuelve un canal con los eventos futuros del trabajo y una
// función para darse de baja. El canal se cierra al darse de baja o si el
// suscriptor se retrasa demasiado; en ese caso debe releer desde el repositorio.
func (h *EventHub) Subscribe(jobID string) (<-chan domain.JobEvent, func()) {
	ch := make(chan domain.JobEvent, subscriberBuffer)
	h.mu.Lock()
	if h.subs[jobID] == nil {
		h.subs[jobID] = map[chan domain.JobEvent]struct{}{}
	}
	h.subs[jobID][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.remove(jobID, ch)
	}
}

// Publish nunca bloquea: un suscriptor con el búfer lleno se descarta.
func (h *EventHub) Publish(event domain.JobEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[event.JobID] {
		select {
		case ch <- event:
		default:
			h.remove(event.JobID, ch)
		}
	}
}

func (h *EventHub) remove(jobID string, ch chan domain.JobEvent) {
	if _, ok := h.subs[jobID][ch]; !ok {
		return
	}
	delete(h.subs[jobID], ch)
	if len(h.subs[jobID]) == 0 {
		delete(h.subs, jobID)
	}
	close(ch)
}
