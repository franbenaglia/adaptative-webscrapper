package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"adaptive-scraper/internal/application"
	"adaptive-scraper/internal/domain"
	"adaptive-scraper/internal/storage/sqlite"
)

const recipeJSON = `{
  "name": "demo",
  "start_url": "https://shop.example/products",
  "item_locator": ".product",
  "fields": [
    {"name": "name", "type": "text", "locator": ".name", "required": true},
    {"name": "price", "type": "number", "locator": ".price"}
  ],
  "pagination": {"type": "next_button", "next_locator": ".next"}
}`

// fakeEngine sustituye al motor real. La CLI llega a él a través de
// application.JobService, el mismo camino que usa la API HTTP.
type fakeEngine struct {
	requests []domain.ScrapeRequest
	records  []domain.Record
	result   domain.ScrapeResult
	err      error
}

func (f *fakeEngine) Run(_ context.Context, request domain.ScrapeRequest, sink application.ProgressSink) (domain.ScrapeResult, error) {
	f.requests = append(f.requests, request)
	if len(f.records) > 0 {
		sink.Emit(application.Event{Type: domain.EventPageOpened, Page: 1, URL: request.URL})
		sink.Emit(application.Event{Type: domain.EventRecordsExtracted, Page: 1, URL: request.URL, Records: len(f.records), NewRecords: f.records})
	}
	return f.result, f.err
}

// harness ejecuta comandos contra una base SQLite temporal que persiste
// entre invocaciones, como ocurre con el binario real.
type harness struct {
	t      *testing.T
	engine *fakeEngine
	dbPath string
	opened int
}

func newHarness(t *testing.T, engine *fakeEngine) *harness {
	return &harness{t: t, engine: engine, dbPath: filepath.Join(t.TempDir(), "cli.db")}
}

func (h *harness) run(args ...string) (stdout, stderr string, err error) {
	var out, errOut bytes.Buffer
	root := NewRootCommand(Deps{
		Version:         "test",
		DefaultHeadless: true,
		Open: func(ctx context.Context) (Services, func(), error) {
			h.opened++
			store, err := sqlite.Open(ctx, h.dbPath)
			if err != nil {
				return Services{}, nil, err
			}
			return Services{
				Jobs:    application.NewJobService(h.engine, store.Jobs(), store.Recipes(), application.JobOptions{}),
				Recipes: application.NewRecipeService(store.Recipes()),
			}, func() { _ = store.Close() }, nil
		},
	})
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	err = root.ExecuteContext(context.Background())
	return out.String(), errOut.String(), err
}

func writeRecipe(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "recipe.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestScrapeRunsThroughSharedJobService(t *testing.T) {
	engine := &fakeEngine{
		records: []domain.Record{{"name": "A", "price": 1.5}},
		result:  domain.ScrapeResult{PagesVisited: 2, Warnings: []string{"algo raro"}},
	}
	h := newHarness(t, engine)

	stdout, stderr, err := h.run("scrape", "--recipe", writeRecipe(t, recipeJSON),
		"--url", "https://shop.example/other", "--max-pages", "2", "--max-records", "7", "--headless=false")
	if err != nil {
		t.Fatal(err)
	}
	if len(engine.requests) != 1 {
		t.Fatalf("el motor se invocó %d veces", len(engine.requests))
	}
	req := engine.requests[0]
	if req.URL != "https://shop.example/other" || req.MaxPages != 2 || req.MaxRecords != 7 || req.Headless || req.Recipe.Name != "demo" {
		t.Errorf("solicitud = %+v", req)
	}
	if !strings.Contains(stdout, `{"name": "A", "price": 1.5}`) {
		t.Errorf("stdout debe contener los datos en JSON: %q", stdout)
	}
	for _, want := range []string{"página 1: https://shop.example/other", "aviso: algo raro", "1 registros en 2 páginas (completed)"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr no contiene %q: %q", want, stderr)
		}
	}

	// La ejecución queda en el historial que también consulta la API.
	list, _, err := h.run("jobs", "list", "--json")
	var jobs []domain.Job
	if err != nil || json.Unmarshal([]byte(list), &jobs) != nil || len(jobs) != 1 ||
		jobs[0].Status != domain.JobCompleted || jobs[0].RecordCount != 1 {
		t.Fatalf("jobs list = %s (%v)", list, err)
	}
	show, _, err := h.run("jobs", "show", jobs[0].ID)
	if err != nil || !strings.Contains(show, `"job.completed"`) || !strings.Contains(show, `"records.extracted"`) {
		t.Errorf("jobs show = %s (%v)", show, err)
	}
	if text, _, _ := h.run("jobs", "list"); !strings.Contains(text, "completed") || !strings.Contains(text, "ESTADO") {
		t.Errorf("tabla de trabajos = %q", text)
	}
}

func TestScrapeWritesCSVFileFromExtension(t *testing.T) {
	h := newHarness(t, &fakeEngine{records: []domain.Record{{"name": "A", "price": 1.5}}})
	output := filepath.Join(t.TempDir(), "out.csv")

	stdout, _, err := h.run("scrape", "--recipe", writeRecipe(t, recipeJSON), "--output", output)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(output)
	if string(data) != "name,price\nA,1.5\n" || stdout != "" {
		t.Errorf("archivo = %q stdout = %q", data, stdout)
	}
}

func TestScrapeExportsPartialResultOnLateFailure(t *testing.T) {
	boom := errors.New("boom")
	h := newHarness(t, &fakeEngine{err: boom, records: []domain.Record{{"name": "A", "price": 1.0}}})

	stdout, stderr, err := h.run("scrape", "--recipe", writeRecipe(t, recipeJSON), "--format", "csv")
	if !errors.Is(err, boom) || !strings.Contains(stdout, "A,1") || !strings.Contains(stderr, "(failed)") {
		t.Errorf("err = %v stdout = %q stderr = %q", err, stdout, stderr)
	}
}

func TestScrapeExitCodeFollowsJobOutcome(t *testing.T) {
	cases := map[int]error{
		ExitBlocked:   fmt.Errorf("captcha: %w", domain.ErrBlocked),
		ExitCancelled: fmt.Errorf("interrumpido: %w", context.Canceled),
		ExitError:     errors.New("boom"),
	}
	for want, engineErr := range cases {
		h := newHarness(t, &fakeEngine{err: engineErr})
		if _, _, err := h.run("scrape", "--recipe", writeRecipe(t, recipeJSON)); ExitCode(err) != want {
			t.Errorf("motor devuelve %v: código %d; se esperaba %d", engineErr, ExitCode(err), want)
		}
	}
}

func TestScrapeRejectsBadInputBeforeRunning(t *testing.T) {
	engine := &fakeEngine{}
	h := newHarness(t, engine)
	valid := writeRecipe(t, recipeJSON)
	cases := map[string][]string{
		"sin receta":          {"scrape"},
		"receta e id":         {"scrape", "--recipe", valid, "--recipe-id", "x"},
		"archivo inexistente": {"scrape", "--recipe", "/no/existe.json"},
		"campo desconocido":   {"scrape", "--recipe", writeRecipe(t, `{"name":"x","code":"alert(1)"}`)},
		"receta inválida":     {"scrape", "--recipe", writeRecipe(t, `{"name":"x"}`)},
		"formato inválido":    {"scrape", "--recipe", valid, "--format", "xml"},
	}
	for name, args := range cases {
		if _, _, err := h.run(args...); err == nil {
			t.Errorf("%s: se esperaba error", name)
		}
	}
	if h.opened != 0 {
		t.Errorf("la entrada inválida no debe abrir la base de datos: %d aperturas", h.opened)
	}
	// Estas llegan al servicio compartido, que es quien las rechaza.
	if _, _, err := h.run("scrape", "--recipe", valid, "--url", "https://evil.example/"); !errors.Is(err, domain.ErrInvalidRequest) {
		t.Errorf("host no permitido = %v", err)
	}
	if _, _, err := h.run("scrape", "--recipe-id", "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("receta inexistente = %v", err)
	}
	if len(engine.requests) != 0 {
		t.Errorf("el motor no debe invocarse con entrada inválida: %d llamadas", len(engine.requests))
	}
	if _, _, err := h.run("scrape", "--recipe", writeRecipe(t, `{"name":"x"}`)); ExitCode(err) != ExitInvalidRecipe {
		t.Errorf("código de salida = %d", ExitCode(err))
	}
}

func TestRecipesImportListShowRun(t *testing.T) {
	engine := &fakeEngine{records: []domain.Record{{"name": "A", "price": 2.0}}}
	h := newHarness(t, engine)

	out, _, err := h.run("recipes", "import", writeRecipe(t, recipeJSON))
	id := strings.TrimSpace(out)
	if err != nil || id == "" {
		t.Fatalf("import = %q, %v", out, err)
	}
	if list, _, _ := h.run("recipes", "list"); !strings.Contains(list, id) || !strings.Contains(list, "demo") {
		t.Errorf("list = %q", list)
	}
	show, _, err := h.run("recipes", "show", id)
	var recipe domain.Recipe
	if err != nil || json.Unmarshal([]byte(show), &recipe) != nil || recipe.ID != id || recipe.Version != 1 {
		t.Errorf("show = %q, %v", show, err)
	}

	stdout, _, err := h.run("recipes", "run", id, "--format", "csv", "--max-pages", "1")
	if err != nil || stdout != "name,price\nA,2\n" {
		t.Fatalf("run = %q, %v", stdout, err)
	}
	if len(engine.requests) != 1 || engine.requests[0].MaxPages != 1 || engine.requests[0].URL != "https://shop.example/products" {
		t.Errorf("solicitud = %+v", engine.requests)
	}
	if _, _, err := h.run("recipes", "show", "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("show inexistente = %v", err)
	}
}

func TestExitCode(t *testing.T) {
	cases := map[int]error{
		ExitOK:            nil,
		ExitError:         errors.New("x"),
		ExitBlocked:       fmt.Errorf("captcha: %w", domain.ErrBlocked),
		ExitCancelled:     fmt.Errorf("run: %w", context.Canceled),
		ExitInvalidRecipe: fmt.Errorf("f.json: %w", domain.ErrInvalidRecipe),
	}
	for want, err := range cases {
		if got := ExitCode(err); got != want {
			t.Errorf("ExitCode(%v) = %d; se esperaba %d", err, got, want)
		}
	}
}

func TestVersionDoesNotOpenServices(t *testing.T) {
	h := newHarness(t, &fakeEngine{})
	if stdout, _, err := h.run("version"); err != nil || stdout != "test\n" || h.opened != 0 {
		t.Errorf("stdout = %q err = %v aperturas = %d", stdout, err, h.opened)
	}
}
