// Package api expone los casos de uso por HTTP. Los handlers solo traducen
// entre HTTP y los servicios de application.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"adaptive-scraper/internal/application"
	"adaptive-scraper/internal/domain"
)

const (
	maxBodyBytes       = 1 << 20
	defaultRecordsPage = 100
	maxRecordsPage     = 1000
	defaultJobsPage    = 50
	sseHeartbeat       = 15 * time.Second
)

// Server agrupa las dependencias de los handlers.
type Server struct {
	Jobs            *application.JobService
	Recipes         *application.RecipeService
	Queue           application.JobQueue
	Logger          *slog.Logger
	DefaultHeadless bool
	// AIStatus informa del proveedor de IA; nil equivale a IA desactivada.
	AIStatus func(ctx context.Context) application.AIStatus
	// AllowedHosts son nombres de host adicionales aceptados en la cabecera
	// Host; loopback y localhost se aceptan siempre.
	AllowedHosts []string
	// DiagnosticsDir es donde se guardan capturas y trazas; vacío las desactiva.
	DiagnosticsDir string
	// WebDir es el directorio con la interfaz web ya compilada; vacío la desactiva.
	WebDir string
}

// Handler devuelve el enrutador completo de la API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /api/v1/health", s.health)
	mux.HandleFunc("GET /api/v1/ai", s.aiStatus)

	mux.HandleFunc("POST /api/v1/jobs", s.createJob)
	mux.HandleFunc("GET /api/v1/jobs", s.listJobs)
	mux.HandleFunc("GET /api/v1/jobs/{id}", s.getJob)
	mux.HandleFunc("POST /api/v1/jobs/{id}/cancel", s.cancelJob)
	mux.HandleFunc("GET /api/v1/jobs/{id}/records", s.jobRecords)
	mux.HandleFunc("GET /api/v1/jobs/{id}/events", s.jobEvents)
	mux.HandleFunc("GET /api/v1/jobs/{id}/export", s.exportJob)
	mux.HandleFunc("GET /api/v1/jobs/{id}/diagnostics", s.listDiagnostics)
	mux.HandleFunc("GET /api/v1/jobs/{id}/diagnostics/{name}", s.getDiagnostic)

	mux.HandleFunc("GET /api/v1/recipes", s.listRecipes)
	mux.HandleFunc("POST /api/v1/recipes", s.createRecipe)
	mux.HandleFunc("GET /api/v1/recipes/{id}", s.getRecipe)
	mux.HandleFunc("PUT /api/v1/recipes/{id}", s.updateRecipe)
	mux.HandleFunc("DELETE /api/v1/recipes/{id}", s.deleteRecipe)
	mux.HandleFunc("POST /api/v1/recipes/{id}/run", s.runRecipe)

	// La interfaz se sirve desde el mismo origen que la API (sin CORS).
	if s.WebDir != "" {
		mux.Handle("GET /", http.FileServer(http.Dir(s.WebDir)))
	}

	return s.recoverPanics(s.logRequests(s.checkHost(requireJSONBody(mux))))
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) aiStatus(w http.ResponseWriter, r *http.Request) {
	status := application.AIStatus{InstalledModels: []string{}}
	if s.AIStatus != nil {
		status = s.AIStatus(r.Context())
	}
	writeJSON(w, http.StatusOK, status)
}

// writeError traduce los errores del dominio a códigos HTTP.
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	var tooLarge *http.MaxBytesError
	switch {
	case errors.Is(err, domain.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, domain.ErrInvalidRecipe), errors.Is(err, domain.ErrInvalidRequest), errors.Is(err, errBadRequest):
		status = http.StatusBadRequest
	case errors.Is(err, domain.ErrInvalidTransition):
		status = http.StatusConflict
	case errors.Is(err, application.ErrQueueFull):
		status = http.StatusServiceUnavailable
	case errors.As(err, &tooLarge):
		status = http.StatusRequestEntityTooLarge
	}
	message := err.Error()
	if status == http.StatusInternalServerError {
		s.Logger.Error("error interno", "method", r.Method, "path", r.URL.Path, "error", err)
		message = "error interno"
	}
	writeJSON(w, status, map[string]string{"error": message})
}

var errBadRequest = errors.New("solicitud mal formada")

func badRequest(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errBadRequest, fmt.Sprintf(format, args...))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// decodeJSON lee un único objeto JSON, sin campos desconocidos y con tamaño
// limitado. Con optional, un cuerpo vacío deja dst sin modificar.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any, optional bool) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if optional && errors.Is(err, io.EOF) {
			return nil
		}
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return err
		}
		return badRequest("JSON inválido: %v", err)
	}
	if dec.More() {
		return badRequest("el cuerpo debe contener un único objeto JSON")
	}
	return nil
}

func queryInt(r *http.Request, name string, fallback, minimum, maximum int) (int, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < minimum || n > maximum {
		return 0, badRequest("%s debe ser un entero entre %d y %d", name, minimum, maximum)
	}
	return n, nil
}

// requireJSONBody exige Content-Type application/json en todos los POST,
// tengan o no cuerpo. Otra web no puede enviar esa cabecera sin preflight
// CORS (PUT y DELETE siempre lo requieren), lo que protege a esta API local
// sin autenticación frente a CSRF.
func requireJSONBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if mediaType != "application/json" {
				writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "se requiere Content-Type: application/json"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// checkHost rechaza peticiones cuya cabecera Host no sea este equipo. Protege
// de DNS rebinding: una web externa que apunte su dominio a 127.0.0.1 llegaría
// a la API, pero con su propio nombre en Host.
func (s *Server) checkHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(r.Host); err == nil {
			host = h
		}
		host = strings.ToLower(strings.Trim(host, "[]"))
		allowed := host == "localhost"
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			allowed = true
		}
		for _, extra := range s.AllowedHosts {
			allowed = allowed || host == extra
		}
		if !allowed {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "cabecera Host no permitida; añádela a APP_ALLOWED_HOSTS si es legítima"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Unwrap permite a http.ResponseController llegar al Flusher original (SSE).
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.Logger.Info("petición", "method", r.Method, "path", r.URL.Path, "status", rec.status, "duration_ms", time.Since(start).Milliseconds())
	})
}

func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil && rec != http.ErrAbortHandler {
				s.Logger.Error("pánico en un handler", "path", r.URL.Path, "panic", rec, "stack", string(debug.Stack()))
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "error interno"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
