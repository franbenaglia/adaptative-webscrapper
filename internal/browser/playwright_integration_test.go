//go:build browser

package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"adaptive-scraper/internal/application"
	"adaptive-scraper/internal/cli"
	"adaptive-scraper/internal/domain"
)

// Ejecuta el servicio real con Chromium contra examples/demo servido en local.
func demoSetup(t *testing.T) (*application.Service, domain.Recipe, string) {
	t.Helper()
	server := httptest.NewServer(http.FileServer(http.Dir("../../examples/demo")))
	t.Cleanup(server.Close)

	recipe, err := cli.LoadRecipe("../../examples/recipes/productos-demo.json")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(server.URL)
	recipe.AllowedHosts = []string{u.Hostname()}

	launcher := NewLauncher()
	t.Cleanup(func() { _ = launcher.Close() })
	return application.NewService(launcher, application.ServiceOptions{}), recipe, server.URL + "/productos.html"
}

func TestDemoSiteTwoPages(t *testing.T) {
	svc, recipe, start := demoSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	result, err := svc.Run(ctx, domain.ScrapeRequest{URL: start, Recipe: &recipe, Headless: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.PagesVisited != 2 || len(result.Records) != 6 {
		t.Fatalf("páginas=%d registros=%d avisos=%v", result.PagesVisited, len(result.Records), result.Warnings)
	}
	first := result.Records[0]
	if first["nombre"] != "Teclado mecánico" || first["precio"] != 45999.90 || first["disponibilidad"] != "En stock" {
		t.Errorf("primer registro = %v", first)
	}
	if link, _ := first["enlace"].(string); link != start[:len(start)-len("/productos.html")]+"/producto/1" {
		t.Errorf("enlace = %v", first["enlace"])
	}
	if result.Records[4]["disponibilidad"] != nil || len(result.Warnings) != 1 {
		t.Errorf("se esperaba un aviso por disponibilidad ausente: %v / %v", result.Records[4], result.Warnings)
	}
}

func TestDemoSiteLimits(t *testing.T) {
	svc, recipe, start := demoSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	result, err := svc.Run(ctx, domain.ScrapeRequest{URL: start, Recipe: &recipe, Headless: true, MaxPages: 1}, nil)
	if err != nil || result.PagesVisited != 1 || len(result.Records) != 3 {
		t.Fatalf("max-pages: páginas=%d registros=%d err=%v", result.PagesVisited, len(result.Records), err)
	}
	result, err = svc.Run(ctx, domain.ScrapeRequest{URL: start, Recipe: &recipe, Headless: true, MaxRecords: 4}, nil)
	if err != nil || result.PagesVisited != 2 || len(result.Records) != 4 {
		t.Fatalf("max-records: páginas=%d registros=%d err=%v", result.PagesVisited, len(result.Records), err)
	}
}

func TestCancellationClosesBrowser(t *testing.T) {
	svc, recipe, start := demoSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := svc.Run(ctx, domain.ScrapeRequest{URL: start, Recipe: &recipe, Headless: true}, nil); err == nil {
		t.Fatal("se esperaba error por cancelación")
	}
}
