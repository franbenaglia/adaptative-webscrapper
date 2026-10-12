package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"adaptive-scraper/internal/application"
	"adaptive-scraper/internal/domain"
	"adaptive-scraper/internal/export"
)

// jobBody es el cuerpo de POST /jobs y POST /recipes/{id}/run.
type jobBody struct {
	URL         string         `json:"url"`
	Instruction string         `json:"instruction"`
	RecipeID    string         `json:"recipe_id"`
	Recipe      *domain.Recipe `json:"recipe"`
	MaxPages    int            `json:"max_pages"`
	MaxRecords  int            `json:"max_records"`
	Headless    *bool          `json:"headless"`
}

func (s *Server) createJob(w http.ResponseWriter, r *http.Request) {
	var body jobBody
	if err := decodeJSON(w, r, &body, false); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.submit(w, r, body)
}

func (s *Server) runRecipe(w http.ResponseWriter, r *http.Request) {
	var body jobBody
	if err := decodeJSON(w, r, &body, true); err != nil {
		s.writeError(w, r, err)
		return
	}
	if body.RecipeID != "" || body.Recipe != nil {
		s.writeError(w, r, badRequest("la receta se indica en la URL, no en el cuerpo"))
		return
	}
	body.RecipeID = r.PathValue("id")
	s.submit(w, r, body)
}

// submit crea el trabajo y lo encola. Responde 202: el resultado se consulta
// después en /jobs/{id} o se sigue por /jobs/{id}/events.
func (s *Server) submit(w http.ResponseWriter, r *http.Request, body jobBody) {
	headless := s.DefaultHeadless
	if body.Headless != nil {
		headless = *body.Headless
	}
	job, err := s.Jobs.Create(r.Context(), application.JobRequest{
		URL: body.URL, Instruction: body.Instruction, RecipeID: body.RecipeID, Recipe: body.Recipe,
		MaxPages: body.MaxPages, MaxRecords: body.MaxRecords, Headless: headless,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.Queue.Submit(job.ID); err != nil {
		if _, rejectErr := s.Jobs.Reject(r.Context(), job.ID, err.Error()); rejectErr != nil {
			s.Logger.Error("no se pudo rechazar un trabajo sin cola", "job_id", job.ID, "error", rejectErr)
		}
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/jobs/"+job.ID)
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	limit, err := queryInt(r, "limit", defaultJobsPage, 1, 1000)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	status := domain.JobStatus(r.URL.Query().Get("status"))
	if status != "" && !status.Valid() {
		s.writeError(w, r, badRequest("estado desconocido %q", status))
		return
	}
	jobs, err := s.Jobs.List(r.Context(), application.JobFilter{Status: status, Limit: limit})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.Jobs.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.Jobs.Cancel(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	// Un trabajo en curso se interrumpe de forma asíncrona.
	status := http.StatusOK
	if !job.Status.Terminal() {
		status = http.StatusAccepted
	}
	writeJSON(w, status, job)
}

func (s *Server) jobRecords(w http.ResponseWriter, r *http.Request) {
	job, err := s.Jobs.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	offset, err := queryInt(r, "offset", 0, 0, domain.LimitMaxRecords)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	limit, err := queryInt(r, "limit", defaultRecordsPage, 1, maxRecordsPage)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	records, err := s.Jobs.Records(r.Context(), job.ID, offset, limit)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"fields": job.Recipe.Fields, "records": records, "total": job.RecordCount, "offset": offset, "limit": limit,
	})
}

func (s *Server) exportJob(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = export.FormatJSON
	}
	exporter, err := export.ForFormat(format)
	if err != nil {
		s.writeError(w, r, badRequest("%v", err))
		return
	}
	job, err := s.Jobs.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	records, err := s.Jobs.Records(r.Context(), job.ID, 0, 0)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	contentType := "application/json; charset=utf-8"
	if format == export.FormatCSV {
		contentType = "text/csv; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="job-%s.%s"`, job.ID, format))
	if err := exporter.Write(w, job.Recipe.Fields, records); err != nil {
		s.Logger.Warn("exportación interrumpida", "job_id", job.ID, "error", err)
	}
}

// jobEvents emite por Server-Sent Events los eventos ya guardados y después
// los nuevos, hasta el evento final del trabajo. El id de cada evento es su
// Seq, así que el navegador reanuda con Last-Event-ID tras una reconexión.
func (s *Server) jobEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if _, err := s.Jobs.Get(ctx, id); err != nil {
		s.writeError(w, r, err)
		return
	}
	var last int64
	if raw := r.Header.Get("Last-Event-ID"); raw != "" {
		last, _ = strconv.ParseInt(raw, 10, 64)
	}

	// Suscribirse antes de leer el historial evita perder eventos entre ambos.
	live, unsubscribe := s.Jobs.Subscribe(id)
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher := http.NewResponseController(w)

	send := func(event domain.JobEvent) (done bool) {
		if event.Seq <= last {
			return false
		}
		last = event.Seq
		data, _ := json.Marshal(event)
		fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.Seq, event.Type, data)
		return event.Terminal()
	}

	past, err := s.Jobs.Events(ctx, id, last)
	if err != nil {
		s.Logger.Error("no se pudieron leer los eventos", "job_id", id, "error", err)
		return
	}
	for _, event := range past {
		if send(event) {
			_ = flusher.Flush()
			return
		}
	}
	if err := flusher.Flush(); err != nil {
		return
	}
	// Un trabajo ya terminado no emitirá nada más.
	if job, err := s.Jobs.Get(ctx, id); err != nil || job.Status.Terminal() {
		return
	}

	heartbeat := time.NewTicker(sseHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-live:
			if !ok {
				return // suscriptor descartado por lento: el cliente reconecta
			}
			done := send(event)
			if err := flusher.Flush(); err != nil || done {
				return
			}
		case <-heartbeat.C:
			fmt.Fprint(w, ": ping\n\n")
			if err := flusher.Flush(); err != nil {
				return
			}
		}
	}
}
