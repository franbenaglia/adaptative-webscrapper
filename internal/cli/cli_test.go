package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"adaptive-scraper/internal/application"
	"adaptive-scraper/internal/domain"
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

// fakeService comprueba que la CLI solo depende de application.ScrapingService,
// el mismo contrato que usará la API HTTP.
type fakeService struct {
	requests []domain.ScrapeRequest
	result   domain.ScrapeResult
	err      error
}

var _ application.ScrapingService = (*fakeService)(nil)

func (f *fakeService) Run(_ context.Context, request domain.ScrapeRequest) (domain.ScrapeResult, error) {
	f.requests = append(f.requests, request)
	result := f.result
	result.Fields = request.Recipe.Fields
	return result, f.err
}

func writeRecipe(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "recipe.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func execute(svc application.ScrapingService, args ...string) (stdout, stderr string, err error) {
	var out, errOut bytes.Buffer
	root := NewRootCommand(Deps{Service: svc, Version: "test", DefaultHeadless: true})
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	err = root.ExecuteContext(context.Background())
	return out.String(), errOut.String(), err
}

func TestScrapeDelegatesToSharedService(t *testing.T) {
	svc := &fakeService{result: domain.ScrapeResult{
		Records:      []domain.Record{{"name": "A", "price": 1.5}},
		PagesVisited: 2,
		Warnings:     []string{"algo raro"},
	}}

	stdout, stderr, err := execute(svc, "scrape", "--recipe", writeRecipe(t, recipeJSON),
		"--url", "https://shop.example/other", "--max-pages", "2", "--max-records", "7", "--headless=false")
	if err != nil {
		t.Fatal(err)
	}
	if len(svc.requests) != 1 {
		t.Fatalf("el servicio se invocó %d veces", len(svc.requests))
	}
	req := svc.requests[0]
	if req.URL != "https://shop.example/other" || req.MaxPages != 2 || req.MaxRecords != 7 || req.Headless || req.Recipe.Name != "demo" {
		t.Errorf("solicitud = %+v", req)
	}
	if !strings.Contains(stdout, `{"name": "A", "price": 1.5}`) {
		t.Errorf("stdout debe contener los datos en JSON: %q", stdout)
	}
	if !strings.Contains(stderr, "aviso: algo raro") || !strings.Contains(stderr, "1 registros en 2 páginas") {
		t.Errorf("stderr debe contener avisos y resumen: %q", stderr)
	}
}

func TestScrapeWritesCSVFileFromExtension(t *testing.T) {
	svc := &fakeService{result: domain.ScrapeResult{Records: []domain.Record{{"name": "A", "price": 1.5}}}}
	output := filepath.Join(t.TempDir(), "out.csv")

	stdout, _, err := execute(svc, "scrape", "--recipe", writeRecipe(t, recipeJSON), "--output", output)
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
	svc := &fakeService{err: boom, result: domain.ScrapeResult{Records: []domain.Record{{"name": "A", "price": 1.0}}}}

	stdout, _, err := execute(svc, "scrape", "--recipe", writeRecipe(t, recipeJSON), "--format", "csv")
	if !errors.Is(err, boom) || !strings.Contains(stdout, "A,1") {
		t.Errorf("err = %v stdout = %q", err, stdout)
	}
}

func TestScrapeRejectsBadInputBeforeCallingService(t *testing.T) {
	svc := &fakeService{}
	cases := map[string][]string{
		"sin receta":          {"scrape"},
		"archivo inexistente": {"scrape", "--recipe", "/no/existe.json"},
		"campo desconocido":   {"scrape", "--recipe", writeRecipe(t, `{"name":"x","code":"alert(1)"}`)},
		"receta inválida":     {"scrape", "--recipe", writeRecipe(t, `{"name":"x"}`)},
		"formato inválido":    {"scrape", "--recipe", writeRecipe(t, recipeJSON), "--format", "xml"},
	}
	for name, args := range cases {
		if _, _, err := execute(svc, args...); err == nil {
			t.Errorf("%s: se esperaba error", name)
		}
	}
	if len(svc.requests) != 0 {
		t.Errorf("el servicio no debe invocarse con entrada inválida: %d llamadas", len(svc.requests))
	}
	if _, _, err := execute(svc, "scrape", "--recipe", writeRecipe(t, `{"name":"x"}`)); ExitCode(err) != ExitInvalidRecipe {
		t.Errorf("código de salida = %d", ExitCode(err))
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

func TestVersion(t *testing.T) {
	if stdout, _, err := execute(&fakeService{}, "version"); err != nil || stdout != "test\n" {
		t.Errorf("stdout = %q err = %v", stdout, err)
	}
}
