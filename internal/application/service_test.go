package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"adaptive-scraper/internal/domain"
)

type fakePage struct {
	url     string
	records []domain.RawRecord
	// locator, si no está vacío, es el único ItemLocator que encuentra elementos.
	locator string
}

// fakeBrowser simula un sitio paginado y registra el uso de la sesión.
type fakeBrowser struct {
	pages       []fakePage
	current     int
	opened      int
	closed      int
	options     BrowserOptions
	extractErr  error
	onExtract   func()
	snapshot    domain.PageSnapshot
	blockReason string
}

func (f *fakeBrowser) Open(_ context.Context, options BrowserOptions) (BrowserSession, error) {
	f.opened++
	f.options = options
	return f, nil
}
func (f *fakeBrowser) Navigate(context.Context, string) error { return nil }
func (f *fakeBrowser) URL() string                            { return f.pages[f.current].url }
func (f *fakeBrowser) Extract(_ context.Context, recipe domain.Recipe) ([]domain.RawRecord, error) {
	if f.onExtract != nil {
		f.onExtract()
	}
	page := f.pages[f.current]
	if page.locator != "" && page.locator != recipe.ItemLocator {
		return nil, f.extractErr
	}
	return page.records, f.extractErr
}
func (f *fakeBrowser) GoToNextPage(context.Context, domain.PaginationRule) (bool, error) {
	if f.current+1 >= len(f.pages) {
		return false, nil
	}
	f.current++
	return true, nil
}
func (f *fakeBrowser) Snapshot(context.Context) (domain.PageSnapshot, error) { return f.snapshot, nil }
func (f *fakeBrowser) BlockReason(context.Context) (string, error)           { return f.blockReason, nil }
func (f *fakeBrowser) Close() error                                          { f.closed++; return nil }

// fakeAI simula el proveedor de IA y cuenta sus invocaciones.
type fakeAI struct {
	discover      func(domain.PageSnapshot, string) (domain.Recipe, error)
	repair        func(domain.Recipe, domain.PageSnapshot, domain.ExtractionFailure) (domain.Recipe, error)
	discoverCalls int
	repairCalls   int
}

func (f *fakeAI) DiscoverRecipe(_ context.Context, snapshot domain.PageSnapshot, instruction string) (domain.Recipe, error) {
	f.discoverCalls++
	return f.discover(snapshot, instruction)
}

func (f *fakeAI) RepairRecipe(_ context.Context, previous domain.Recipe, snapshot domain.PageSnapshot, failure domain.ExtractionFailure) (domain.Recipe, error) {
	f.repairCalls++
	return f.repair(previous, snapshot, failure)
}

func recipe() *domain.Recipe {
	return &domain.Recipe{
		Name:        "demo",
		StartURL:    "https://shop.example/p1",
		ItemLocator: ".product",
		Fields: []domain.FieldDefinition{
			{Name: "name", Type: domain.FieldText, Required: true},
			{Name: "price", Type: domain.FieldNumber, Required: true},
		},
		Pagination: domain.PaginationRule{Type: domain.PaginationNextButton, NextLocator: ".next"},
	}
}

func threePages() *fakeBrowser {
	return &fakeBrowser{pages: []fakePage{
		{url: "https://shop.example/p1", records: []domain.RawRecord{{"name": "A", "price": "$ 1,50"}, {"name": "B", "price": "2"}}},
		{url: "https://shop.example/p2", records: []domain.RawRecord{{"name": "C", "price": "3"}, {"name": "D"}}},
		{url: "https://shop.example/p3", records: []domain.RawRecord{{"name": "E", "price": "5"}}},
	}}
}

func TestRunWalksAllPages(t *testing.T) {
	browser := threePages()
	var events []Event
	svc := NewService(browser, ServiceOptions{})
	sink := ProgressFunc(func(e Event) { events = append(events, e) })

	result, err := svc.Run(context.Background(), domain.ScrapeRequest{Recipe: recipe(), Headless: true}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if result.PagesVisited != 3 || len(result.Records) != 5 {
		t.Fatalf("páginas=%d registros=%d", result.PagesVisited, len(result.Records))
	}
	if result.Records[0]["price"] != 1.5 || result.Records[3]["price"] != nil {
		t.Errorf("conversión incorrecta: %v", result.Records)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], `"price" ausente en 1 de 5`) {
		t.Errorf("avisos = %v", result.Warnings)
	}
	if browser.opened != 1 || browser.closed != 1 || !browser.options.Headless {
		t.Errorf("sesión mal gestionada: %+v", browser)
	}
	if len(events) != 6 || events[0].Type != domain.EventPageOpened || events[5].Records != 5 || len(events[3].NewRecords) != 2 {
		t.Errorf("eventos = %+v", events)
	}
}

func TestRunRespectsLimits(t *testing.T) {
	result, err := NewService(threePages(), ServiceOptions{}).Run(context.Background(), domain.ScrapeRequest{Recipe: recipe(), MaxPages: 2}, nil)
	if err != nil || result.PagesVisited != 2 || len(result.Records) != 4 {
		t.Fatalf("max-pages: páginas=%d registros=%d err=%v", result.PagesVisited, len(result.Records), err)
	}

	result, err = NewService(threePages(), ServiceOptions{}).Run(context.Background(), domain.ScrapeRequest{Recipe: recipe(), MaxRecords: 3}, nil)
	if err != nil || result.PagesVisited != 2 || len(result.Records) != 3 {
		t.Fatalf("max-records: páginas=%d registros=%d err=%v", result.PagesVisited, len(result.Records), err)
	}
}

func TestRunWithoutPaginationReadsOnePage(t *testing.T) {
	r := recipe()
	r.Pagination = domain.PaginationRule{}
	result, err := NewService(threePages(), ServiceOptions{}).Run(context.Background(), domain.ScrapeRequest{Recipe: r}, nil)
	if err != nil || result.PagesVisited != 1 {
		t.Fatalf("páginas=%d err=%v", result.PagesVisited, err)
	}
}

func TestRunStopsWhenPaginationRepeatsContent(t *testing.T) {
	same := []domain.RawRecord{{"name": "A", "price": "1"}}
	browser := &fakeBrowser{pages: []fakePage{{url: "https://shop.example/p1", records: same}, {url: "https://shop.example/p1", records: same}, {url: "https://shop.example/p1", records: same}}}

	result, err := NewService(browser, ServiceOptions{}).Run(context.Background(), domain.ScrapeRequest{Recipe: recipe()}, nil)
	if err != nil || result.PagesVisited != 1 || len(result.Records) != 1 || len(result.Warnings) != 1 {
		t.Fatalf("resultado = %+v err=%v", result, err)
	}
}

func TestRunStopsWhenLeavingAllowedHosts(t *testing.T) {
	browser := threePages()
	browser.pages[1].url = "https://evil.example/p2"

	result, err := NewService(browser, ServiceOptions{}).Run(context.Background(), domain.ScrapeRequest{Recipe: recipe()}, nil)
	if err == nil || !strings.Contains(err.Error(), "hosts permitidos") {
		t.Fatalf("se esperaba error de host, se obtuvo %v", err)
	}
	if len(result.Records) != 2 || browser.closed != 1 {
		t.Errorf("debe conservar lo extraído y cerrar la sesión: registros=%d cierres=%d", len(result.Records), browser.closed)
	}
}

func TestRunClosesSessionOnFailureAndKeepsPartialResult(t *testing.T) {
	browser := threePages()
	boom := errors.New("boom")
	calls := 0
	browser.onExtract = func() {
		if calls++; calls == 2 {
			browser.extractErr = boom
		}
	}

	result, err := NewService(browser, ServiceOptions{}).Run(context.Background(), domain.ScrapeRequest{Recipe: recipe()}, nil)
	if !errors.Is(err, boom) || len(result.Records) != 2 || browser.closed != 1 {
		t.Fatalf("err=%v registros=%d cierres=%d", err, len(result.Records), browser.closed)
	}
}

func TestRunStopsOnCancellation(t *testing.T) {
	browser := threePages()
	ctx, cancel := context.WithCancel(context.Background())
	browser.onExtract = cancel

	result, err := NewService(browser, ServiceOptions{}).Run(ctx, domain.ScrapeRequest{Recipe: recipe()}, nil)
	if !errors.Is(err, context.Canceled) || result.PagesVisited != 1 || browser.closed != 1 {
		t.Fatalf("err=%v páginas=%d cierres=%d", err, result.PagesVisited, browser.closed)
	}
}

func TestRunRejectsInvalidRequestWithoutOpeningBrowser(t *testing.T) {
	browser := threePages()
	_, err := NewService(browser, ServiceOptions{}).Run(context.Background(), domain.ScrapeRequest{}, nil)
	if !errors.Is(err, domain.ErrInvalidRequest) || browser.opened != 0 {
		t.Fatalf("err=%v aperturas=%d", err, browser.opened)
	}
}

// --- Descubrimiento y reparación con IA ---

var oneGroup = domain.PageSnapshot{URL: "https://shop.example/p1", Groups: []domain.CandidateGroup{{ID: "g1", Selector: ".card", Count: 2}}}

// proposal es lo que devolvería el proveedor: solo selectores y campos.
func proposal(itemLocator string) domain.Recipe {
	return domain.Recipe{
		ItemLocator: itemLocator,
		Fields: []domain.FieldDefinition{
			{Name: "name", Type: domain.FieldText, Locator: ".title", Required: true},
			{Name: "price", Type: domain.FieldNumber, Locator: ".cost", Required: true},
		},
		Pagination: domain.PaginationRule{Type: domain.PaginationNone},
	}
}

func eventNames(events []Event) string {
	names := make([]string, len(events))
	for i, e := range events {
		names[i] = e.Type
	}
	return strings.Join(names, " ")
}

func cardsSite() *fakeBrowser {
	return &fakeBrowser{snapshot: oneGroup, pages: []fakePage{
		{url: "https://www.shop.example/p1", locator: ".card", records: []domain.RawRecord{{"name": "A", "price": "1"}, {"name": "B", "price": "2"}}},
	}}
}

func TestRunDiscoversRecipeFromInstruction(t *testing.T) {
	browser := cardsSite()
	ai := &fakeAI{discover: func(snapshot domain.PageSnapshot, instruction string) (domain.Recipe, error) {
		if len(snapshot.Groups) != 1 || instruction != "nombre y precio de cada producto" {
			t.Errorf("el proveedor recibió %+v / %q", snapshot, instruction)
		}
		return proposal(".card"), nil
	}}
	var events []Event
	svc := NewService(browser, ServiceOptions{Intelligence: ai, MaxRepairs: 2})

	result, err := svc.Run(context.Background(), domain.ScrapeRequest{
		URL: "https://shop.example/p1", Instruction: "nombre y precio de cada producto",
	}, ProgressFunc(func(e Event) { events = append(events, e) }))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 2 || result.Records[1]["price"] != 2.0 || result.RecipeOrigin != domain.RecipeDiscovered {
		t.Fatalf("resultado = %+v", result)
	}
	// El motor completa la receta con datos propios y la valida.
	r := result.Recipe
	if r == nil || r.Validate() != nil || r.StartURL != "https://www.shop.example/p1" || r.Name != "nombre y precio de cada producto" ||
		strings.Join(r.AllowedHosts, ",") != "shop.example,www.shop.example" {
		t.Errorf("receta final = %+v", r)
	}
	if got := eventNames(events); got != "page.opened recipe.discovered recipe.validated records.extracted" {
		t.Errorf("eventos = %q", got)
	}
	if events[1].Recipe == nil || events[1].Recipe.ItemLocator != ".card" || ai.discoverCalls != 1 || ai.repairCalls != 0 {
		t.Errorf("evento de receta = %+v, llamadas = %d/%d", events[1], ai.discoverCalls, ai.repairCalls)
	}
}

func TestRunWithoutRecipeNeedsAI(t *testing.T) {
	browser := cardsSite()
	_, err := NewService(browser, ServiceOptions{}).Run(context.Background(),
		domain.ScrapeRequest{URL: "https://shop.example/p1", Instruction: "nombres"}, nil)
	if !errors.Is(err, domain.ErrInvalidRequest) || browser.opened != 0 {
		t.Fatalf("err = %v, aperturas = %d", err, browser.opened)
	}
}

func TestRunRejectsUnsafeRecipeFromModel(t *testing.T) {
	for name, mutate := range map[string]func(*domain.Recipe){
		"motor xpath":       func(r *domain.Recipe) { r.ItemLocator = "xpath=//div" },
		"tipo desconocido":  func(r *domain.Recipe) { r.Fields[0].Type = "script" },
		"sin campos":        func(r *domain.Recipe) { r.Fields = nil },
		"selector excesivo": func(r *domain.Recipe) { r.Fields[0].Locator = strings.Repeat("a", domain.MaxSelectorLength+1) },
	} {
		browser := cardsSite()
		ai := &fakeAI{discover: func(domain.PageSnapshot, string) (domain.Recipe, error) {
			r := proposal(".card")
			mutate(&r)
			return r, nil
		}}
		result, err := NewService(browser, ServiceOptions{Intelligence: ai}).Run(context.Background(),
			domain.ScrapeRequest{URL: "https://shop.example/p1", Instruction: "nombres"}, nil)
		if !errors.Is(err, domain.ErrInvalidRecipe) || len(result.Records) != 0 || browser.closed != 1 {
			t.Errorf("%s: err = %v, registros = %d", name, err, len(result.Records))
		}
	}
}

func TestRunFailsWhenDiscoveryFindsNothing(t *testing.T) {
	empty := cardsSite()
	empty.snapshot = domain.PageSnapshot{}
	ai := &fakeAI{}
	_, err := NewService(empty, ServiceOptions{Intelligence: ai}).Run(context.Background(),
		domain.ScrapeRequest{URL: "https://shop.example/p1", Instruction: "nombres"}, nil)
	if err == nil || ai.discoverCalls != 0 {
		t.Errorf("sin candidatos no debe consultarse al modelo: err = %v, llamadas = %d", err, ai.discoverCalls)
	}

	// El modelo elige mal y las reparaciones tampoco aciertan.
	browser := cardsSite()
	ai = &fakeAI{
		discover: func(domain.PageSnapshot, string) (domain.Recipe, error) { return proposal(".wrong"), nil },
		repair: func(domain.Recipe, domain.PageSnapshot, domain.ExtractionFailure) (domain.Recipe, error) {
			return proposal(".also-wrong"), nil
		},
	}
	result, err := NewService(browser, ServiceOptions{Intelligence: ai, MaxRepairs: 2}).Run(context.Background(),
		domain.ScrapeRequest{URL: "https://shop.example/p1", Instruction: "nombres"}, nil)
	if err == nil || !strings.Contains(err.Error(), "no extrae nada") || ai.repairCalls != 2 || len(result.Warnings) != 2 {
		t.Errorf("err = %v, reparaciones = %d, avisos = %v", err, ai.repairCalls, result.Warnings)
	}
}

// Con una receta que funciona, el modelo no interviene nunca.
func TestRunWithWorkingRecipeNeverCallsAI(t *testing.T) {
	ai := &fakeAI{}
	var events []Event
	result, err := NewService(threePages(), ServiceOptions{Intelligence: ai, MaxRepairs: 2}).Run(context.Background(),
		domain.ScrapeRequest{Recipe: recipe()}, ProgressFunc(func(e Event) { events = append(events, e) }))
	if err != nil || len(result.Records) != 5 || result.RecipeOrigin != domain.RecipeProvided {
		t.Fatalf("resultado = %+v, err = %v", result, err)
	}
	if ai.discoverCalls != 0 || ai.repairCalls != 0 || strings.Contains(eventNames(events), "recipe.") {
		t.Errorf("llamadas = %d/%d, eventos = %q", ai.discoverCalls, ai.repairCalls, eventNames(events))
	}
}

func brokenRecipe() *domain.Recipe {
	r := proposal(".old-card")
	r.ID, r.Name, r.Version, r.StartURL = "r1", "Productos", 3, "https://www.shop.example/p1"
	return &r
}

func TestRunRepairsBrokenRecipe(t *testing.T) {
	browser := cardsSite()
	ai := &fakeAI{repair: func(previous domain.Recipe, _ domain.PageSnapshot, failure domain.ExtractionFailure) (domain.Recipe, error) {
		if previous.ItemLocator != ".old-card" || failure.Items != 0 {
			t.Errorf("reparación recibió %+v / %+v", previous, failure)
		}
		return proposal(".card"), nil
	}}
	var events []Event

	result, err := NewService(browser, ServiceOptions{Intelligence: ai, MaxRepairs: 2}).Run(context.Background(),
		domain.ScrapeRequest{Recipe: brokenRecipe()}, ProgressFunc(func(e Event) { events = append(events, e) }))
	if err != nil {
		t.Fatal(err)
	}
	r := result.Recipe
	if len(result.Records) != 2 || result.RecipeOrigin != domain.RecipeRepaired || ai.repairCalls != 1 ||
		r.ItemLocator != ".card" || r.ID != "r1" || r.Name != "Productos" || r.Version != 3 || r.StartURL != "https://www.shop.example/p1" {
		t.Fatalf("resultado = %+v, receta = %+v", result, r)
	}
	if got := eventNames(events); got != "page.opened recipe.repairing recipe.validated records.extracted" {
		t.Errorf("eventos = %q", got)
	}
}

func TestRunRepairsRecipeWithMissingRequiredField(t *testing.T) {
	browser := cardsSite()
	browser.pages[0].locator = ""
	browser.pages[0].records = []domain.RawRecord{{"name": "A"}, {"name": "B"}} // falta price en todos
	fixed := []domain.RawRecord{{"name": "A", "price": "1"}, {"name": "B", "price": "2"}}
	ai := &fakeAI{repair: func(_ domain.Recipe, _ domain.PageSnapshot, failure domain.ExtractionFailure) (domain.Recipe, error) {
		if failure.Missing["price"] != 2 {
			t.Errorf("fallo = %+v", failure)
		}
		browser.pages[0].records = fixed
		return proposal(".old-card"), nil
	}}

	result, err := NewService(browser, ServiceOptions{Intelligence: ai, MaxRepairs: 1}).Run(context.Background(),
		domain.ScrapeRequest{Recipe: brokenRecipe()}, nil)
	if err != nil || len(result.Records) != 2 || result.Records[0]["price"] != 1.0 || result.RecipeOrigin != domain.RecipeRepaired {
		t.Fatalf("resultado = %+v, err = %v", result, err)
	}
}

func TestRunLimitsRepairAttemptsAndKeepsFieldNames(t *testing.T) {
	cases := map[string]func(domain.Recipe, domain.PageSnapshot, domain.ExtractionFailure) (domain.Recipe, error){
		"cambia los campos": func(domain.Recipe, domain.PageSnapshot, domain.ExtractionFailure) (domain.Recipe, error) {
			r := proposal(".card")
			r.Fields[1].Name = "coste"
			return r, nil
		},
		"sigue sin funcionar": func(domain.Recipe, domain.PageSnapshot, domain.ExtractionFailure) (domain.Recipe, error) {
			return proposal(".nope"), nil
		},
		"el modelo falla": func(domain.Recipe, domain.PageSnapshot, domain.ExtractionFailure) (domain.Recipe, error) {
			return domain.Recipe{}, errors.New("modelo no disponible")
		},
	}
	for name, repair := range cases {
		browser := cardsSite()
		ai := &fakeAI{repair: repair}
		result, err := NewService(browser, ServiceOptions{Intelligence: ai, MaxRepairs: 2}).Run(context.Background(),
			domain.ScrapeRequest{Recipe: brokenRecipe()}, nil)
		// Una receta recibida que no se pudo reparar termina sin datos, con avisos.
		if err != nil || ai.repairCalls != 2 || len(result.Records) != 0 || result.RecipeOrigin != domain.RecipeProvided ||
			result.Recipe.ItemLocator != ".old-card" || len(result.Warnings) < 2 {
			t.Errorf("%s: err = %v, reparaciones = %d, resultado = %+v", name, err, ai.repairCalls, result)
		}
	}
}

func TestRunStopsAsBlockedInsteadOfRepairing(t *testing.T) {
	ai := &fakeAI{}

	withRecipe := cardsSite()
	withRecipe.blockReason = "la página exige iniciar sesión"
	_, err := NewService(withRecipe, ServiceOptions{Intelligence: ai, MaxRepairs: 2}).Run(context.Background(),
		domain.ScrapeRequest{Recipe: brokenRecipe()}, nil)
	if !errors.Is(err, domain.ErrBlocked) || !strings.Contains(err.Error(), "iniciar sesión") {
		t.Errorf("con receta: err = %v", err)
	}

	discovering := cardsSite()
	discovering.blockReason = "la página muestra un CAPTCHA"
	_, err = NewService(discovering, ServiceOptions{Intelligence: ai, MaxRepairs: 2}).Run(context.Background(),
		domain.ScrapeRequest{URL: "https://shop.example/p1", Instruction: "nombres"}, nil)
	if !errors.Is(err, domain.ErrBlocked) {
		t.Errorf("descubriendo: err = %v", err)
	}
	if ai.discoverCalls != 0 || ai.repairCalls != 0 {
		t.Errorf("ante un bloqueo no se consulta al modelo: %d/%d", ai.discoverCalls, ai.repairCalls)
	}

	// Sin IA también se detecta el bloqueo cuando la receta no encuentra nada.
	noAI := cardsSite()
	noAI.blockReason = "la página muestra un CAPTCHA"
	if _, err := NewService(noAI, ServiceOptions{}).Run(context.Background(), domain.ScrapeRequest{Recipe: brokenRecipe()}, nil); !errors.Is(err, domain.ErrBlocked) {
		t.Errorf("sin IA: err = %v", err)
	}
}
