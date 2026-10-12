package api_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"adaptive-scraper/internal/api"
	"adaptive-scraper/internal/application"
	"adaptive-scraper/internal/domain"
	"adaptive-scraper/internal/storage/sqlite"
)

const recipeJSON = `{
  "name": "demo",
  "start_url": "https://shop.example/p1",
  "item_locator": ".product",
  "fields": [
    {"name": "name", "type": "text", "locator": ".name", "required": true},
    {"name": "price", "type": "number", "locator": ".price"}
  ],
  "pagination": {"type": "next_button", "next_locator": ".next"}
}`

// engine simula el motor: dos páginas y tres registros. Si gate no es nil,
// espera entre páginas a que se cierre o a la cancelación.
type engine struct {
	gate     chan struct{}
	requests chan domain.ScrapeRequest
}

func (e *engine) Run(ctx context.Context, request domain.ScrapeRequest, sink application.ProgressSink) (domain.ScrapeResult, error) {
	e.requests <- request
	emit := func(page, total int, records ...domain.Record) {
		url := fmt.Sprintf("https://shop.example/p%d", page)
		sink.Emit(application.Event{Type: domain.EventPageOpened, Page: page, URL: url})
		sink.Emit(application.Event{Type: domain.EventRecordsExtracted, Page: page, URL: url, Records: total, NewRecords: records})
	}
	emit(1, 2, domain.Record{"name": "A", "price": 1.5}, domain.Record{"name": "B", "price": nil})
	if e.gate != nil {
		select {
		case <-e.gate:
		case <-ctx.Done():
			return domain.ScrapeResult{PagesVisited: 1}, ctx.Err()
		}
	}
	emit(2, 3, domain.Record{"name": "C", "price": 3.0})
	return domain.ScrapeResult{PagesVisited: 2, Warnings: []string{"aviso"}}, nil
}

type testServer struct {
	t      *testing.T
	url    string
	engine *engine
}

func newServer(t *testing.T, gated bool, queue func(*application.JobService) application.JobQueue) *testServer {
	t.Helper()
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	eng := &engine{requests: make(chan domain.ScrapeRequest, 16)}
	if gated {
		eng.gate = make(chan struct{})
	}
	jobs := application.NewJobService(eng, store.Jobs(), store.Recipes(), application.JobOptions{AIEnabled: true})
	ctx, cancel := context.WithCancel(context.Background())
	var q application.JobQueue
	wait := func() {}
	if queue != nil {
		q = queue(jobs)
	} else {
		real := application.NewQueue(jobs, 2, 16)
		real.Start(ctx)
		q, wait = real, real.Wait
	}
	server := httptest.NewServer((&api.Server{
		Jobs: jobs, Recipes: application.NewRecipeService(store.Recipes()), Queue: q,
		Logger: slog.New(slog.DiscardHandler), DefaultHeadless: true,
		AIStatus: func(context.Context) application.AIStatus {
			return application.AIStatus{Enabled: true, Provider: "ollama", Model: "mini", Reachable: true, ModelInstalled: true, InstalledModels: []string{"mini"}}
		},
	}).Handler())
	t.Cleanup(func() {
		server.CloseClientConnections()
		server.Close()
		cancel()
		wait()
		_ = store.Close()
	})
	return &testServer{t: t, url: server.URL, engine: eng}
}

// do envía una petición y decodifica la respuesta JSON en out (si no es nil).
func (s *testServer) do(method, path, body string, out any) *http.Response {
	s.t.Helper()
	req, err := http.NewRequest(method, s.url+path, strings.NewReader(body))
	if err != nil {
		s.t.Fatal(err)
	}
	if method == http.MethodPost || method == http.MethodPut {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			s.t.Fatalf("%s %s: respuesta no es JSON (%d): %s", method, path, resp.StatusCode, data)
		}
	}
	resp.Body = io.NopCloser(strings.NewReader(string(data)))
	return resp
}

func (s *testServer) createJob(body string) domain.Job {
	s.t.Helper()
	var job domain.Job
	if resp := s.do("POST", "/api/v1/jobs", body, &job); resp.StatusCode != http.StatusAccepted {
		s.t.Fatalf("POST /jobs = %d", resp.StatusCode)
	}
	return job
}

func (s *testServer) waitFor(id string, status domain.JobStatus) domain.Job {
	s.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var job domain.Job
		s.do("GET", "/api/v1/jobs/"+id, "", &job)
		if job.Status == status {
			return job
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("el trabajo %s sigue en %s; se esperaba %s", id, job.Status, status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type sseEvent struct {
	ID, Event string
	Data      domain.JobEvent
}

// stream lee eventos SSE hasta que el servidor cierra la respuesta.
func (s *testServer) stream(id, lastEventID string) []sseEvent {
	s.t.Helper()
	req, _ := http.NewRequest("GET", s.url+"/api/v1/jobs/"+id+"/events", nil)
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		s.t.Fatalf("Content-Type = %q", ct)
	}
	var events []sseEvent
	var current sseEvent
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "id: "):
			current.ID = line[4:]
		case strings.HasPrefix(line, "event: "):
			current.Event = line[7:]
		case strings.HasPrefix(line, "data: "):
			if err := json.Unmarshal([]byte(line[6:]), &current.Data); err != nil {
				s.t.Fatalf("data inválido: %s", line)
			}
		case line == "" && current.Event != "":
			events = append(events, current)
			current = sseEvent{}
		}
	}
	if err := scanner.Err(); err != nil {
		s.t.Fatalf("el flujo SSE no terminó limpiamente: %v", err)
	}
	return events
}

func names(events []sseEvent) string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.Event
	}
	return strings.Join(out, " ")
}

const allEvents = "job.started page.opened records.extracted page.opened records.extracted job.completed"

func TestHealth(t *testing.T) {
	s := newServer(t, false, nil)
	for _, path := range []string{"/healthz", "/api/v1/health"} {
		var body map[string]string
		if resp := s.do("GET", path, "", &body); resp.StatusCode != 200 || body["status"] != "ok" {
			t.Errorf("%s = %d %v", path, resp.StatusCode, body)
		}
	}
}

func TestJobLifecycle(t *testing.T) {
	s := newServer(t, false, nil)

	created := s.createJob(`{"recipe": ` + recipeJSON + `, "max_pages": 3, "max_records": 50}`)
	if created.ID == "" || created.Status != domain.JobQueued || !created.Headless || created.MaxPages != 3 {
		t.Fatalf("trabajo creado = %+v", created)
	}
	if req := <-s.engine.requests; req.MaxRecords != 50 || req.Recipe.Name != "demo" || req.URL != "https://shop.example/p1" {
		t.Errorf("el motor recibió %+v", req)
	}
	job := s.waitFor(created.ID, domain.JobCompleted)
	if job.RecordCount != 3 || job.PagesVisited != 2 || len(job.Warnings) != 1 || job.FinishedAt == nil {
		t.Errorf("trabajo final = %+v", job)
	}

	var page struct {
		Fields  []domain.FieldDefinition
		Records []domain.Record
		Total   int
		Offset  int
		Limit   int
	}
	s.do("GET", "/api/v1/jobs/"+job.ID+"/records?offset=1&limit=1", "", &page)
	if page.Total != 3 || len(page.Records) != 1 || page.Records[0]["name"] != "B" || len(page.Fields) != 2 {
		t.Errorf("página de registros = %+v", page)
	}

	csv := s.do("GET", "/api/v1/jobs/"+job.ID+"/export?format=csv", "", nil)
	body, _ := io.ReadAll(csv.Body)
	if string(body) != "name,price\nA,1.5\nB,\nC,3\n" || !strings.Contains(csv.Header.Get("Content-Disposition"), ".csv") {
		t.Errorf("CSV = %q (%s)", body, csv.Header.Get("Content-Disposition"))
	}
	var exported []domain.Record
	s.do("GET", "/api/v1/jobs/"+job.ID+"/export", "", &exported)
	if len(exported) != 3 || exported[2]["price"] != 3.0 {
		t.Errorf("JSON exportado = %v", exported)
	}

	var list struct{ Jobs []domain.Job }
	s.do("GET", "/api/v1/jobs?status=completed&limit=10", "", &list)
	if len(list.Jobs) != 1 || list.Jobs[0].ID != job.ID {
		t.Errorf("listado = %+v", list.Jobs)
	}
	s.do("GET", "/api/v1/jobs?status=queued", "", &list)
	if len(list.Jobs) != 0 {
		t.Errorf("no debería haber trabajos en cola: %+v", list.Jobs)
	}
}

func TestEventsReplayForFinishedJobAndResume(t *testing.T) {
	s := newServer(t, false, nil)
	job := s.waitFor(s.createJob(`{"recipe": `+recipeJSON+`}`).ID, domain.JobCompleted)

	events := s.stream(job.ID, "")
	if names(events) != allEvents {
		t.Fatalf("eventos = %q", names(events))
	}
	if events[0].ID != "1" || events[5].Data.Records != 3 || events[2].Data.Page != 1 || events[1].Data.URL == "" {
		t.Errorf("contenido de eventos = %+v", events)
	}
	if resumed := s.stream(job.ID, "4"); names(resumed) != "records.extracted job.completed" || resumed[0].ID != "5" {
		t.Errorf("reanudación = %q", names(resumed))
	}
}

func TestEventsStreamLiveUntilJobEnds(t *testing.T) {
	s := newServer(t, true, nil)
	job := s.createJob(`{"recipe": ` + recipeJSON + `}`)
	<-s.engine.requests

	result := make(chan []sseEvent, 1)
	go func() { result <- s.stream(job.ID, "") }()
	// El trabajo sigue detenido entre páginas: el cliente SSE queda a la espera.
	select {
	case events := <-result:
		t.Fatalf("el flujo terminó antes que el trabajo: %q", names(events))
	case <-time.After(150 * time.Millisecond):
	}
	close(s.engine.gate)

	select {
	case events := <-result:
		if names(events) != allEvents {
			t.Errorf("eventos = %q", names(events))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("el flujo SSE no terminó con el trabajo")
	}
}

func TestCancelRunningAndFinishedJob(t *testing.T) {
	s := newServer(t, true, nil)
	job := s.createJob(`{"recipe": ` + recipeJSON + `}`)
	<-s.engine.requests
	s.waitFor(job.ID, domain.JobRunning)

	if resp := s.do("POST", "/api/v1/jobs/"+job.ID+"/cancel", "", nil); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("cancelar en curso = %d", resp.StatusCode)
	}
	cancelled := s.waitFor(job.ID, domain.JobCancelled)
	if cancelled.RecordCount != 2 {
		t.Errorf("debe conservar lo extraído antes de cancelar: %+v", cancelled)
	}
	if resp := s.do("POST", "/api/v1/jobs/"+job.ID+"/cancel", "", nil); resp.StatusCode != http.StatusConflict {
		t.Errorf("cancelar un trabajo terminado = %d", resp.StatusCode)
	}
	if events := s.stream(job.ID, ""); !strings.HasSuffix(names(events), "job.cancelled") {
		t.Errorf("eventos = %q", names(events))
	}
}

type fullQueue struct{}

func (fullQueue) Submit(string) error { return application.ErrQueueFull }

func TestQueueFullRejectsJob(t *testing.T) {
	s := newServer(t, false, func(*application.JobService) application.JobQueue { return fullQueue{} })

	if resp := s.do("POST", "/api/v1/jobs", `{"recipe": `+recipeJSON+`}`, nil); resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("estado = %d", resp.StatusCode)
	}
	var list struct{ Jobs []domain.Job }
	s.do("GET", "/api/v1/jobs", "", &list)
	if len(list.Jobs) != 1 || list.Jobs[0].Status != domain.JobFailed || list.Jobs[0].Error == "" {
		t.Errorf("el trabajo rechazado debe quedar como fallido: %+v", list.Jobs)
	}
}

func TestRecipesCRUDAndRun(t *testing.T) {
	s := newServer(t, false, nil)

	var created domain.Recipe
	resp := s.do("POST", "/api/v1/recipes", recipeJSON, &created)
	if resp.StatusCode != http.StatusCreated || created.ID == "" || created.Version != 1 || resp.Header.Get("Location") == "" {
		t.Fatalf("crear = %d %+v", resp.StatusCode, created)
	}
	path := "/api/v1/recipes/" + created.ID

	var updated domain.Recipe
	changed := strings.Replace(recipeJSON, `".product"`, `".card"`, 1)
	if resp := s.do("PUT", path, changed, &updated); resp.StatusCode != 200 || updated.Version != 2 || updated.ItemLocator != ".card" {
		t.Fatalf("actualizar = %d %+v", resp.StatusCode, updated)
	}
	var fetched domain.Recipe
	s.do("GET", path, "", &fetched)
	var list struct{ Recipes []domain.Recipe }
	s.do("GET", "/api/v1/recipes", "", &list)
	if fetched.Version != 2 || len(list.Recipes) != 1 {
		t.Errorf("leer = %+v, listar = %+v", fetched, list.Recipes)
	}

	var job domain.Job
	if resp := s.do("POST", path+"/run", `{"max_pages": 1, "headless": false}`, &job); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("run = %d", resp.StatusCode)
	}
	if job.RecipeID != created.ID || job.Headless || job.MaxPages != 1 || job.Recipe.ItemLocator != ".card" {
		t.Errorf("trabajo = %+v", job)
	}
	s.waitFor(job.ID, domain.JobCompleted)
	if resp := s.do("POST", path+"/run", "", &job); resp.StatusCode != http.StatusAccepted {
		t.Errorf("run sin cuerpo = %d", resp.StatusCode)
	}
	s.waitFor(job.ID, domain.JobCompleted)

	if resp := s.do("DELETE", path, "", nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("borrar = %d", resp.StatusCode)
	}
	if resp := s.do("GET", path, "", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("leer borrada = %d", resp.StatusCode)
	}
	// El historial conserva su copia de la receta.
	var old domain.Job
	if s.do("GET", "/api/v1/jobs/"+job.ID, "", &old); old.Recipe.Name != "demo" {
		t.Errorf("el trabajo perdió su receta: %+v", old)
	}
}

func TestErrorResponses(t *testing.T) {
	s := newServer(t, false, nil)
	cases := []struct {
		name, method, path, body string
		want                     int
	}{
		{"JSON roto", "POST", "/api/v1/jobs", `{"recipe": `, 400},
		{"campo desconocido", "POST", "/api/v1/jobs", `{"script": "alert(1)"}`, 400},
		{"dos objetos", "POST", "/api/v1/jobs", `{} {}`, 400},
		{"sin receta ni instrucción", "POST", "/api/v1/jobs", `{"url": "https://shop.example/"}`, 400},
		{"instrucción sin URL", "POST", "/api/v1/jobs", `{"instruction": "nombres"}`, 400},
		{"receta inválida", "POST", "/api/v1/jobs", `{"recipe": {"name": "x"}}`, 400},
		{"host no permitido", "POST", "/api/v1/jobs", `{"url": "https://evil.example/", "recipe": ` + recipeJSON + `}`, 400},
		{"receta guardada inexistente", "POST", "/api/v1/jobs", `{"recipe_id": "nope"}`, 404},
		{"cuerpo enorme", "POST", "/api/v1/recipes", `{"name": "` + strings.Repeat("x", 2<<20) + `"}`, 413},
		{"receta inválida al guardar", "POST", "/api/v1/recipes", `{"name": "x"}`, 400},
		{"actualizar inexistente", "PUT", "/api/v1/recipes/nope", recipeJSON, 404},
		{"borrar inexistente", "DELETE", "/api/v1/recipes/nope", "", 404},
		{"run con receta en el cuerpo", "POST", "/api/v1/recipes/x/run", `{"recipe_id": "y"}`, 400},
		{"run de receta inexistente", "POST", "/api/v1/recipes/nope/run", `{}`, 404},
		{"trabajo inexistente", "GET", "/api/v1/jobs/nope", "", 404},
		{"registros de trabajo inexistente", "GET", "/api/v1/jobs/nope/records", "", 404},
		{"eventos de trabajo inexistente", "GET", "/api/v1/jobs/nope/events", "", 404},
		{"exportar trabajo inexistente", "GET", "/api/v1/jobs/nope/export", "", 404},
		{"cancelar inexistente", "POST", "/api/v1/jobs/nope/cancel", "", 404},
		{"estado desconocido", "GET", "/api/v1/jobs?status=raro", "", 400},
		{"límite inválido", "GET", "/api/v1/jobs?limit=0", "", 400},
		{"ruta desconocida", "GET", "/api/v1/nada", "", 404},
	}
	for _, c := range cases {
		var body map[string]string
		resp := s.do(c.method, c.path, c.body, nil)
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != c.want {
			t.Errorf("%s: estado %d; se esperaba %d (%s)", c.name, resp.StatusCode, c.want, data)
			continue
		}
		if c.path != "/api/v1/nada" && (json.Unmarshal(data, &body) != nil || body["error"] == "") {
			t.Errorf("%s: el cuerpo debe ser {\"error\": ...}: %s", c.name, data)
		}
	}

	var job domain.Job
	s.do("GET", "/api/v1/jobs/"+s.waitFor(s.createJob(`{"recipe": `+recipeJSON+`}`).ID, domain.JobCompleted).ID, "", &job)
	for _, query := range []string{"format=xml", ""} {
		want := 400
		if query == "" {
			want = 200
		}
		if resp := s.do("GET", "/api/v1/jobs/"+job.ID+"/export?"+query, "", nil); resp.StatusCode != want {
			t.Errorf("export?%s = %d", query, resp.StatusCode)
		}
	}
	if resp := s.do("GET", "/api/v1/jobs/"+job.ID+"/records?limit=5000", "", nil); resp.StatusCode != 400 {
		t.Errorf("limit excesivo = %d", resp.StatusCode)
	}
}

// Un POST sin Content-Type JSON es lo que enviaría un formulario de otra web.
func TestPostRequiresJSONContentType(t *testing.T) {
	s := newServer(t, false, nil)
	for _, contentType := range []string{"", "text/plain", "application/x-www-form-urlencoded"} {
		req, _ := http.NewRequest("POST", s.url+"/api/v1/jobs", strings.NewReader(`{"recipe": `+recipeJSON+`}`))
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnsupportedMediaType {
			t.Errorf("Content-Type %q = %d", contentType, resp.StatusCode)
		}
	}
	var list struct{ Jobs []domain.Job }
	if s.do("GET", "/api/v1/jobs", "", &list); len(list.Jobs) != 0 {
		t.Errorf("no debe crearse ningún trabajo: %d", len(list.Jobs))
	}
}

func TestServesWebInterfaceWhenConfigured(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<title>AdaptiveScraper</title>"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer((&api.Server{Logger: slog.New(slog.DiscardHandler), WebDir: dir}).Handler())
	defer server.Close()

	for path, want := range map[string]int{"/": 200, "/no-existe.js": 404, "/healthz": 200} {
		resp, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s = %d; se esperaba %d", path, resp.StatusCode, want)
		}
		if path == "/" && !strings.Contains(string(body), "AdaptiveScraper") {
			t.Errorf("/ no sirve index.html: %s", body)
		}
	}
}

func TestJobFromInstructionAndAIStatus(t *testing.T) {
	s := newServer(t, false, nil)

	var status application.AIStatus
	if resp := s.do("GET", "/api/v1/ai", "", &status); resp.StatusCode != 200 || !status.Enabled || status.Model != "mini" || !status.ModelInstalled {
		t.Fatalf("estado de la IA = %d %+v", resp.StatusCode, status)
	}

	job := s.createJob(`{"url": "https://shop.example/p1", "instruction": "nombre y precio de cada producto"}`)
	if job.Instruction != "nombre y precio de cada producto" || job.Recipe.Fields == nil || len(job.Recipe.Fields) != 0 {
		t.Errorf("trabajo creado = %+v", job)
	}
	if req := <-s.engine.requests; req.Recipe != nil || req.Instruction != "nombre y precio de cada producto" {
		t.Errorf("el motor recibió %+v", req)
	}
	s.waitFor(job.ID, domain.JobCompleted)
}

func TestAIStatusWhenDisabled(t *testing.T) {
	server := httptest.NewServer((&api.Server{Logger: slog.New(slog.DiscardHandler)}).Handler())
	defer server.Close()
	resp, err := http.Get(server.URL + "/api/v1/ai")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"enabled":false`) || !strings.Contains(string(body), `"installed_models":[]`) {
		t.Errorf("respuesta = %d %s", resp.StatusCode, body)
	}
}

// Una web externa que apunte su dominio a 127.0.0.1 llega con su nombre en Host.
func TestRejectsForeignHostHeader(t *testing.T) {
	handler := (&api.Server{Logger: slog.New(slog.DiscardHandler), AllowedHosts: []string{"scraper.lan"}}).Handler()
	cases := map[string]int{
		"127.0.0.1:8080": 200, "localhost:8080": 200, "LOCALHOST": 200, "[::1]:8080": 200, "scraper.lan:8080": 200,
		"evil.example": 403, "evil.example:8080": 403, "127.0.0.1.evil.example": 403, "192.168.1.5:8080": 403, "": 403,
	}
	for host, want := range cases {
		req := httptest.NewRequest("GET", "/healthz", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("Host %q = %d; se esperaba %d", host, rec.Code, want)
		}
	}
}

func TestDiagnosticsEndpoints(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "diag.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	eng := &engine{requests: make(chan domain.ScrapeRequest, 4)}
	jobs := application.NewJobService(eng, store.Jobs(), store.Recipes(), application.JobOptions{})
	var recipe domain.Recipe
	_ = json.Unmarshal([]byte(recipeJSON), &recipe)
	job, err := jobs.Create(ctx, application.JobRequest{Recipe: &recipe})
	if err != nil {
		t.Fatal(err)
	}
	diagDir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(diagDir, job.ID), 0o700)
	_ = os.WriteFile(filepath.Join(diagDir, job.ID, "screenshot.png"), []byte("PNG-DATA"), 0o600)
	_ = os.WriteFile(filepath.Join(diagDir, job.ID, "secreto.txt"), []byte("no"), 0o600)
	_ = os.WriteFile(filepath.Join(diagDir, "fuera.txt"), []byte("no"), 0o600)

	get := func(server *httptest.Server, path string) (int, string) {
		resp, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}
	on := httptest.NewServer((&api.Server{Jobs: jobs, Logger: slog.New(slog.DiscardHandler), DiagnosticsDir: diagDir}).Handler())
	defer on.Close()
	base := "/api/v1/jobs/" + job.ID + "/diagnostics"

	if code, body := get(on, base); code != 200 || !strings.Contains(body, `"files":["screenshot.png"]`) {
		t.Errorf("listado = %d %s", code, body)
	}
	if code, body := get(on, base+"/screenshot.png"); code != 200 || body != "PNG-DATA" {
		t.Errorf("captura = %d %q", code, body)
	}
	for _, name := range []string{"trace.zip", "secreto.txt", "..%2Ffuera.txt", "%2e%2e%2f%2e%2e%2ffuera.txt"} {
		if code, body := get(on, base+"/"+name); code != 404 {
			t.Errorf("%s = %d %q; se esperaba 404", name, code, body)
		}
	}
	if code, _ := get(on, "/api/v1/jobs/nope/diagnostics"); code != 404 {
		t.Errorf("trabajo inexistente = %d", code)
	}

	off := httptest.NewServer((&api.Server{Jobs: jobs, Logger: slog.New(slog.DiscardHandler)}).Handler())
	defer off.Close()
	if code, body := get(off, base); code != 200 || !strings.Contains(body, `"files":[]`) {
		t.Errorf("listado con diagnósticos desactivados = %d %s", code, body)
	}
	if code, _ := get(off, base+"/screenshot.png"); code != 404 {
		t.Errorf("captura con diagnósticos desactivados = %d", code)
	}
}
