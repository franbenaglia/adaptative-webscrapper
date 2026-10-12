//go:build browser && ollama

package browser

import (
	"context"
	"os"
	"testing"
	"time"

	"adaptive-scraper/internal/application"
	"adaptive-scraper/internal/cli"
	"adaptive-scraper/internal/config"
	"adaptive-scraper/internal/domain"
	"adaptive-scraper/internal/intelligence"
)

// Estas pruebas usan un modelo real servido por Ollama. Sirven para comprobar
// que un modelo concreto es capaz de hacer el trabajo antes de adoptarlo:
//
//	OLLAMA_MODEL=llama3.2:3b make test-ai

func realModelService(t *testing.T) *application.Service {
	t.Helper()
	model := os.Getenv("OLLAMA_MODEL")
	if model == "" {
		model = config.DefaultOllamaModel
	}
	baseURL := os.Getenv("OLLAMA_BASE_URL")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:11434"
	}
	ollama := intelligence.NewOllama(baseURL, model, 10*time.Minute)
	if status := ollama.Status(context.Background()); !status.Reachable || !status.ModelInstalled {
		t.Fatalf("modelo %s no disponible: %s", model, status.Message)
	}
	t.Logf("modelo: %s", model)
	launcher := NewLauncher(localOptions)
	t.Cleanup(func() { _ = launcher.Close() })
	return application.NewService(launcher, application.ServiceOptions{Intelligence: intelligence.NewProvider(ollama), MaxRepairs: 2})
}

func logEvents(t *testing.T, start time.Time) application.ProgressSink {
	return application.ProgressFunc(func(e application.Event) {
		t.Logf("%6.1fs %s %s", time.Since(start).Seconds(), e.Type, e.Message)
	})
}

func TestRealModelDiscoversProductCards(t *testing.T) {
	svc := realModelService(t)
	start := time.Now()
	result, err := svc.Run(context.Background(), domain.ScrapeRequest{
		URL: demoServer(t) + "/productos.html", Instruction: "Extraer nombre, precio y disponibilidad de cada producto", Headless: true,
	}, logEvents(t, start))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("receta: %+v", *result.Recipe)
	t.Logf("avisos: %v", result.Warnings)
	if len(result.Records) != 6 || result.PagesVisited != 2 {
		t.Errorf("registros=%d páginas=%d", len(result.Records), result.PagesVisited)
	}
	// Un modelo pequeño añade a veces una columna no pedida (el enlace); se tolera.
	if result.Recipe.ItemLocator != "article.product" || len(result.Recipe.Fields) < 3 || len(result.Recipe.Fields) > 4 {
		t.Errorf("receta inesperada: %d campos sobre %q", len(result.Recipe.Fields), result.Recipe.ItemLocator)
	}
	values := map[any]bool{}
	for _, v := range result.Records[0] {
		values[v] = true
	}
	for _, want := range []any{"Teclado mecánico", 45999.90, "En stock"} {
		if !values[want] {
			t.Errorf("el primer registro no contiene %v: %v", want, result.Records[0])
		}
	}
}

func TestRealModelDiscoversTableRows(t *testing.T) {
	svc := realModelService(t)
	start := time.Now()
	result, err := svc.Run(context.Background(), domain.ScrapeRequest{
		URL: demoServer(t) + "/tabla.html", Instruction: "producto y precio de cada fila", Headless: true,
	}, logEvents(t, start))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("receta: %+v", *result.Recipe)
	if len(result.Records) != 5 || len(result.Recipe.Fields) < 2 || len(result.Recipe.Fields) > 3 {
		t.Fatalf("registros=%d campos=%d", len(result.Records), len(result.Recipe.Fields))
	}
	values := map[any]bool{}
	for _, v := range result.Records[1] {
		values[v] = true
	}
	if !values["Mouse inalámbrico"] || !values[12500.0] {
		t.Errorf("segunda fila = %v", result.Records[1])
	}
}

func TestRealModelRepairsRecipeAfterRedesign(t *testing.T) {
	svc := realModelService(t)
	recipe, err := cli.LoadRecipe("../../examples/recipes/productos-demo.json")
	if err != nil {
		t.Fatal(err)
	}
	recipe.StartURL = demoServer(t) + "/productos-v2.html"
	start := time.Now()
	result, err := svc.Run(context.Background(), domain.ScrapeRequest{Recipe: &recipe, Headless: true}, logEvents(t, start))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("receta: %+v", *result.Recipe)
	t.Logf("avisos: %v", result.Warnings)
	if result.RecipeOrigin != domain.RecipeRepaired || len(result.Records) != 4 {
		t.Fatalf("origen=%s registros=%d", result.RecipeOrigin, len(result.Records))
	}
	last := result.Records[3]
	if last["nombre"] != "Auriculares" || last["precio"] != 30250.50 || last["disponibilidad"] != "En stock" || last["enlace"] == nil {
		t.Errorf("último registro = %v", last)
	}
}

// Variante con solo tres campos: con ella llama3.2:3b llegó a proponer el
// enlace como precio. La reparación debe acertar o fallar, nunca dar por
// buenos datos equivocados.
func TestRealModelRepairNeverAcceptsWrongData(t *testing.T) {
	svc := realModelService(t)
	recipe, err := cli.LoadRecipe("../../examples/recipes/productos-demo.json")
	if err != nil {
		t.Fatal(err)
	}
	recipe.StartURL = demoServer(t) + "/productos-v2.html"
	recipe.Fields = recipe.Fields[:3]
	start := time.Now()
	result, err := svc.Run(context.Background(), domain.ScrapeRequest{Recipe: &recipe, Headless: true}, logEvents(t, start))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("origen: %s, avisos: %v", result.RecipeOrigin, result.Warnings)
	if result.RecipeOrigin != domain.RecipeRepaired {
		t.Skipf("el modelo no logró reparar la receta (resultado seguro, sin datos): %v", result.Warnings)
	}
	first := result.Records[0]
	if first["nombre"] != "Teclado mecánico" || first["precio"] != 45999.90 || first["disponibilidad"] != "En stock" {
		t.Errorf("la reparación se aceptó con datos erróneos: %v", first)
	}
}
