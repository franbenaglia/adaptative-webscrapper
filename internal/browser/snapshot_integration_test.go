//go:build browser

package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"adaptive-scraper/internal/application"
	"adaptive-scraper/internal/cli"
	"adaptive-scraper/internal/domain"
	"adaptive-scraper/internal/intelligence"
)

func demoServer(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.FileServer(http.Dir("../../examples/demo")))
	t.Cleanup(server.Close)
	return server.URL
}

func openPage(t *testing.T, url string) application.BrowserSession {
	t.Helper()
	launcher := NewLauncher(localOptions)
	t.Cleanup(func() { _ = launcher.Close() })
	session, err := launcher.Open(context.Background(), application.BrowserOptions{
		Headless: true, NavigationTimeout: 30 * time.Second, ActionTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	if err := session.Navigate(context.Background(), url); err != nil {
		t.Fatal(err)
	}
	return session
}

func fieldWithSample(group domain.CandidateGroup, sample, attribute string) *domain.CandidateField {
	for i, f := range group.Fields {
		if f.Attribute == attribute && strings.Contains(strings.Join(f.Samples, "|"), sample) {
			return &group.Fields[i]
		}
	}
	return nil
}

func TestSnapshotFindsProductCards(t *testing.T) {
	session := openPage(t, demoServer(t)+"/productos.html")
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Groups) == 0 || snapshot.Groups[0].Selector != "article.product" || snapshot.Groups[0].Count != 3 {
		t.Fatalf("grupos = %+v", snapshot.Groups)
	}
	cards := snapshot.Groups[0]
	name, price := fieldWithSample(cards, "Teclado mecánico", ""), fieldWithSample(cards, "$ 45.999,90", "")
	stock, link := fieldWithSample(cards, "En stock", ""), fieldWithSample(cards, "/producto/1", "href")
	if name == nil || price == nil || stock == nil || link == nil {
		t.Fatalf("faltan campos candidatos: %+v", cards.Fields)
	}
	if price.Coverage != 3 || len(price.Samples) != 3 || cards.ID != "g1" || !strings.HasPrefix(price.ID, "g1.f") {
		t.Errorf("campo de precio = %+v", price)
	}
	if len(snapshot.Next) != 1 || snapshot.Next[0].Selector != "button.next" || snapshot.Next[0].Text != "Siguiente" {
		t.Errorf("paginación candidata = %+v", snapshot.Next)
	}
	if !strings.Contains(snapshot.Title, "página 1") || !strings.Contains(snapshot.Text, "Teclado mecánico") || len(snapshot.Text) > 1600 {
		t.Errorf("título/texto = %q / %d caracteres", snapshot.Title, len(snapshot.Text))
	}

	// Cada candidato debe poder usarse tal cual en una receta válida.
	recipe := domain.Recipe{
		Name: "x", StartURL: session.URL(), ItemLocator: cards.Selector,
		Fields: []domain.FieldDefinition{
			{Name: "name", Type: domain.FieldText, Locator: name.Selector},
			{Name: "link", Type: domain.FieldURL, Locator: link.Selector, Attribute: link.Attribute},
		},
		Pagination: domain.PaginationRule{Type: domain.PaginationNextButton, NextLocator: snapshot.Next[0].Selector},
	}
	if err := recipe.Validate(); err != nil {
		t.Fatalf("los candidatos no forman una receta válida: %v", err)
	}
	raws, err := session.Extract(context.Background(), recipe)
	if err != nil || len(raws) != 3 || raws[0]["name"] != "Teclado mecánico" || raws[2]["link"] != "/producto/3" {
		t.Errorf("extracción con candidatos = %v, %v", raws, err)
	}
}

func TestSnapshotFindsTableRows(t *testing.T) {
	session := openPage(t, demoServer(t)+"/tabla.html")
	snapshot, err := session.Snapshot(context.Background())
	if err != nil || len(snapshot.Groups) == 0 {
		t.Fatalf("snapshot = %+v, %v", snapshot, err)
	}
	rows := snapshot.Groups[0]
	if rows.Selector != "#precios > tbody > tr" || rows.Count != 5 {
		t.Fatalf("grupo de filas = %+v", snapshot.Groups)
	}
	product, price := fieldWithSample(rows, "Mouse inalámbrico", ""), fieldWithSample(rows, "12500.00", "")
	if product == nil || price == nil || product.Selector == price.Selector {
		t.Fatalf("celdas candidatas = %+v", rows.Fields)
	}
	recipe := domain.Recipe{ItemLocator: rows.Selector, Fields: []domain.FieldDefinition{
		{Name: "product", Locator: product.Selector}, {Name: "price", Locator: price.Selector},
	}}
	raws, err := session.Extract(context.Background(), recipe)
	if err != nil || len(raws) != 5 || raws[4]["product"] != "Webcam HD" || raws[4]["price"] != "22000.00" {
		t.Errorf("extracción de la tabla = %v, %v", raws, err)
	}
}

func TestBlockReason(t *testing.T) {
	base := demoServer(t)
	login := openPage(t, base+"/login.html")
	if reason, err := login.BlockReason(context.Background()); err != nil || !strings.Contains(reason, "iniciar sesión") {
		t.Errorf("login: %q, %v", reason, err)
	}
	products := openPage(t, base+"/productos.html")
	if reason, err := products.BlockReason(context.Background()); err != nil || reason != "" {
		t.Errorf("página normal: %q, %v", reason, err)
	}
}

func TestNavigateReportsHTTPErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/forbidden" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	launcher := NewLauncher(localOptions)
	defer launcher.Close()
	session, err := launcher.Open(context.Background(), application.BrowserOptions{Headless: true, NavigationTimeout: 30 * time.Second, ActionTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	if err := session.Navigate(context.Background(), server.URL+"/forbidden"); !errors.Is(err, domain.ErrBlocked) {
		t.Errorf("403 = %v", err)
	}
	if err := session.Navigate(context.Background(), server.URL+"/missing"); err == nil || errors.Is(err, domain.ErrBlocked) {
		t.Errorf("404 = %v", err)
	}
}

// keywordLLM hace de modelo: lee el prompt real y elige candidatos por el
// contenido de sus muestras. Permite probar todo el recorrido sin Ollama.
type keywordLLM struct{ calls int }

var candidateLine = regexp.MustCompile(`(?m)^  (g\d+\.f\d+) \((text|number|attribute \w+, url)\) samples: (.*)$`)

func (l *keywordLLM) Complete(_ context.Context, _, user string, _ json.RawMessage) (string, error) {
	l.calls++
	wanted := []struct{ name, kind, sample, fieldType string }{
		{"nombre", "text", "Teclado", "text"}, {"precio", "number", "45.999", "number"}, {"disponibilidad", "text", "En stock", "text"},
	}
	var fields []string
	group := ""
	for _, w := range wanted {
		for _, m := range candidateLine.FindAllStringSubmatch(user, -1) {
			if m[2] == w.kind && strings.Contains(m[3], w.sample) {
				group = strings.Split(m[1], ".")[0]
				fields = append(fields, fmt.Sprintf(`{"name": %q, "candidate": %q, "type": %q, "required": true}`, w.name, m[1], w.fieldType))
				break
			}
		}
	}
	next := "none"
	if m := regexp.MustCompile(`(?m)^  (n\d+): "Siguiente"`).FindStringSubmatch(user); m != nil {
		next = m[1]
	}
	return fmt.Sprintf(`{"group": %q, "next": %q, "fields": [%s]}`, group, next, strings.Join(fields, ",")), nil
}

func aiService(t *testing.T) (*application.Service, *keywordLLM) {
	t.Helper()
	launcher := NewLauncher(localOptions)
	t.Cleanup(func() { _ = launcher.Close() })
	llm := &keywordLLM{}
	return application.NewService(launcher, application.ServiceOptions{Intelligence: intelligence.NewProvider(llm), MaxRepairs: 2}), llm
}

func TestDiscoverRecipeOnDemoSite(t *testing.T) {
	svc, llm := aiService(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	result, err := svc.Run(ctx, domain.ScrapeRequest{
		URL: demoServer(t) + "/productos.html", Instruction: "nombre, precio y disponibilidad", Headless: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.PagesVisited != 2 || len(result.Records) != 6 || result.RecipeOrigin != domain.RecipeDiscovered || llm.calls != 1 {
		t.Fatalf("páginas=%d registros=%d origen=%s llamadas=%d avisos=%v", result.PagesVisited, len(result.Records), result.RecipeOrigin, llm.calls, result.Warnings)
	}
	first := result.Records[0]
	if first["nombre"] != "Teclado mecánico" || first["precio"] != 45999.90 || first["disponibilidad"] != "En stock" {
		t.Errorf("primer registro = %v", first)
	}
	if result.Recipe.ItemLocator != "article.product" || !result.Recipe.HasPagination() || result.Recipe.Validate() != nil {
		t.Errorf("receta descubierta = %+v", result.Recipe)
	}
}

func TestRepairRecipeAfterRedesign(t *testing.T) {
	svc, llm := aiService(t)
	base := demoServer(t)
	recipe, err := cli.LoadRecipe("../../examples/recipes/productos-demo.json")
	if err != nil {
		t.Fatal(err)
	}
	recipe.StartURL = base + "/productos-v2.html"
	recipe.Fields = recipe.Fields[:3] // nombre, precio, disponibilidad
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	result, err := svc.Run(ctx, domain.ScrapeRequest{Recipe: &recipe, Headless: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 4 || result.RecipeOrigin != domain.RecipeRepaired || llm.calls != 1 {
		t.Fatalf("registros=%d origen=%s llamadas=%d avisos=%v", len(result.Records), result.RecipeOrigin, llm.calls, result.Warnings)
	}
	if last := result.Records[3]; last["nombre"] != "Auriculares" || last["precio"] != 30250.50 || last["disponibilidad"] != "En stock" {
		t.Errorf("último registro = %v", last)
	}
	if result.Recipe.ItemLocator != "div.card" || result.Recipe.Name != recipe.Name {
		t.Errorf("receta reparada = %+v", result.Recipe)
	}
}

func TestLoginPageEndsAsBlocked(t *testing.T) {
	svc, llm := aiService(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	_, err := svc.Run(ctx, domain.ScrapeRequest{URL: demoServer(t) + "/login.html", Instruction: "productos", Headless: true}, nil)
	if !errors.Is(err, domain.ErrBlocked) || llm.calls != 0 {
		t.Fatalf("err = %v, llamadas = %d", err, llm.calls)
	}
}
