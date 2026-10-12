package intelligence

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"adaptive-scraper/internal/domain"
)

var snapshot = domain.PageSnapshot{
	URL: "https://shop.example/list", Title: "Tienda",
	Groups: []domain.CandidateGroup{
		{ID: "g1", Selector: "article.product", Count: 6, Fields: []domain.CandidateField{
			{ID: "g1.f1", Selector: "h2.name", Samples: []string{"Teclado mecánico", "Mouse"}, Coverage: 6},
			{ID: "g1.f2", Selector: "h2.name > a", Attribute: "href", Samples: []string{"/producto/1"}, Coverage: 6},
			{ID: "g1.f3", Selector: "span.price", Samples: []string{"$ 45.999,90", "$ 12.500,00"}, Coverage: 6},
			{ID: "g1.f4", Selector: "span.stock", Samples: []string{"En stock", "Sin stock"}, Coverage: 5},
		}},
		{ID: "g2", Selector: "nav > a", Count: 4, Fields: []domain.CandidateField{
			{ID: "g2.f1", Selector: "", Samples: []string{"Inicio", "Ofertas"}, Coverage: 4},
		}},
	},
	Next: []domain.NextCandidate{{ID: "n1", Selector: "button.next", Text: "Siguiente"}},
}

// scriptedLLM devuelve una respuesta fija y guarda lo que recibió.
type scriptedLLM struct {
	reply        string
	err          error
	system, user string
	schema       json.RawMessage
}

func (l *scriptedLLM) Complete(_ context.Context, system, user string, schema json.RawMessage) (string, error) {
	l.system, l.user, l.schema = system, user, schema
	return l.reply, l.err
}

const goodReply = `{"group": "g1", "next": "n1", "fields": [
  {"name": "Nombre", "candidate": "g1.f1", "type": "text", "required": true},
  {"name": "precio", "candidate": "g1.f3", "type": "number", "required": true},
  {"name": "Disponibilidad ", "candidate": "g1.f4", "type": "number", "required": false},
  {"name": "enlace", "candidate": "g1.f2", "type": "text", "required": false}
]}`

func TestDiscoverBuildsRecipeFromCandidateIDs(t *testing.T) {
	llm := &scriptedLLM{reply: goodReply}

	recipe, err := NewProvider(llm).DiscoverRecipe(context.Background(), snapshot, "nombre, precio y disponibilidad")
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.FieldDefinition{
		{Name: "nombre", Type: domain.FieldText, Locator: "h2.name", Required: true},
		{Name: "precio", Type: domain.FieldNumber, Locator: "span.price", Required: true},
		// El tipo sale de los datos del candidato, no de lo que diga el modelo.
		{Name: "disponibilidad", Type: domain.FieldText, Locator: "span.stock"},
		{Name: "enlace", Type: domain.FieldURL, Locator: "h2.name > a", Attribute: "href"},
	}
	if recipe.ItemLocator != "article.product" || len(recipe.Fields) != len(want) {
		t.Fatalf("receta = %+v", recipe)
	}
	for i, f := range want {
		if recipe.Fields[i] != f {
			t.Errorf("campo %d = %+v; se esperaba %+v", i, recipe.Fields[i], f)
		}
	}
	if recipe.Pagination != (domain.PaginationRule{Type: domain.PaginationNextButton, NextLocator: "button.next"}) {
		t.Errorf("paginación = %+v", recipe.Pagination)
	}

	// El prompt lleva la instrucción y los candidatos, pero ningún selector.
	for _, wanted := range []string{"nombre, precio y disponibilidad", "GROUP g1: 6 repeated items", `g1.f3 (number) samples: "$ 45.999,90"`, "g1.f2 (attribute href, url)", `g1.f4 (text) samples: "En stock"`, `n1: "Siguiente"`} {
		if !strings.Contains(llm.user, wanted) {
			t.Errorf("el prompt no contiene %q:\n%s", wanted, llm.user)
		}
	}
	if strings.Contains(llm.user, "article.product") || strings.Contains(llm.user, "span.price") {
		t.Errorf("el prompt no debe exponer selectores:\n%s", llm.user)
	}
	// El esquema cierra los identificadores posibles.
	var schema struct {
		Properties struct {
			Group struct{ Enum []string }
			Next  struct{ Enum []string }
		}
	}
	if err := json.Unmarshal(llm.schema, &schema); err != nil ||
		strings.Join(schema.Properties.Group.Enum, ",") != "g1,g2" || strings.Join(schema.Properties.Next.Enum, ",") != "none,n1" {
		t.Errorf("esquema = %s (%v)", llm.schema, err)
	}
}

func TestDiscoverWithoutPagination(t *testing.T) {
	reply := `{"group": "g1", "next": "none", "fields": [{"name": "nombre", "candidate": "g1.f1", "type": "text", "required": true}]}`
	recipe, err := NewProvider(&scriptedLLM{reply: reply}).DiscoverRecipe(context.Background(), snapshot, "nombres")
	if err != nil || recipe.Pagination.Type != domain.PaginationNone || recipe.HasPagination() {
		t.Fatalf("receta = %+v, err = %v", recipe, err)
	}
}

// La respuesta del modelo es entrada no confiable.
func TestDiscoverRejectsBadModelOutput(t *testing.T) {
	field := `{"name": "n", "candidate": "g1.f1", "type": "text", "required": true}`
	cases := map[string]string{
		"no es JSON":             `Claro, aquí tienes la receta: {`,
		"campo desconocido":      `{"group": "g1", "next": "none", "fields": [` + field + `], "script": "alert(1)"}`,
		"dos objetos":            `{"group": "g1", "next": "none", "fields": [` + field + `]} {"group": "g2"}`,
		"grupo inexistente":      `{"group": "g9", "next": "none", "fields": [` + field + `]}`,
		"campo de otro grupo":    `{"group": "g1", "next": "none", "fields": [{"name": "n", "candidate": "g2.f1", "type": "text", "required": true}]}`,
		"candidato inventado":    `{"group": "g1", "next": "none", "fields": [{"name": "n", "candidate": "div.secret", "type": "text", "required": true}]}`,
		"sin campos":             `{"group": "g1", "next": "none", "fields": []}`,
		"paginación inexistente": `{"group": "g1", "next": "n7", "fields": [` + field + `]}`,
		"solo nombres inválidos": `{"group": "g1", "next": "none", "fields": [{"name": "¿?", "candidate": "g1.f1", "type": "text", "required": true}]}`,
		"respuesta desmesurada":  `{"group": "g1", "next": "none", "fields": [` + field + `]}` + strings.Repeat(" ", maxResponseBytes),
		"demasiados campos":      `{"group": "g1", "next": "none", "fields": [` + strings.TrimSuffix(strings.Repeat(field+",", domain.MaxFields+1), ",") + `]}`,
	}
	for name, reply := range cases {
		if recipe, err := NewProvider(&scriptedLLM{reply: reply}).DiscoverRecipe(context.Background(), snapshot, "x"); err == nil {
			t.Errorf("%s: se aceptó %+v", name, recipe)
		}
	}
	boom := errors.New("sin conexión")
	if _, err := NewProvider(&scriptedLLM{err: boom}).DiscoverRecipe(context.Background(), snapshot, "x"); !errors.Is(err, boom) {
		t.Errorf("error del modelo = %v", err)
	}
}

func TestDiscoverDropsDuplicateNames(t *testing.T) {
	reply := `{"group": "g1", "next": "none", "fields": [
	  {"name": "precio", "candidate": "g1.f3", "type": "number", "required": true},
	  {"name": "Precio", "candidate": "g1.f1", "type": "text", "required": true}]}`
	recipe, err := NewProvider(&scriptedLLM{reply: reply}).DiscoverRecipe(context.Background(), snapshot, "precio")
	if err != nil || len(recipe.Fields) != 1 || recipe.Fields[0].Locator != "span.price" {
		t.Fatalf("receta = %+v, err = %v", recipe, err)
	}
}

func TestRepairKeepsFieldNamesTypesAndRequired(t *testing.T) {
	previous := domain.Recipe{ItemLocator: "div.old", Fields: []domain.FieldDefinition{
		{Name: "nombre", Type: domain.FieldText, Locator: ".old-name", Required: true},
		{Name: "precio", Type: domain.FieldNumber, Locator: ".old-price", Required: true},
	}}
	failure := domain.ExtractionFailure{Items: 0}
	llm := &scriptedLLM{reply: `{"group": "g1", "next": "n1", "fields": [
	  {"name": "precio", "candidate": "g1.f3", "type": "text", "required": false},
	  {"name": "nombre", "candidate": "g1.f1", "type": "text", "required": false}]}`}

	recipe, err := NewProvider(llm).RepairRecipe(context.Background(), previous, snapshot, failure)
	if err != nil {
		t.Fatal(err)
	}
	if recipe.ItemLocator != "article.product" || len(recipe.Fields) != 2 ||
		recipe.Fields[0] != (domain.FieldDefinition{Name: "precio", Type: domain.FieldNumber, Locator: "span.price", Required: true}) ||
		recipe.Fields[1] != (domain.FieldDefinition{Name: "nombre", Type: domain.FieldText, Locator: "h2.name", Required: true}) {
		t.Fatalf("receta = %+v", recipe)
	}
	for _, wanted := range []string{"ningún elemento coincide", "- nombre (text, required)", "- precio (number, required)"} {
		if !strings.Contains(llm.user, wanted) {
			t.Errorf("el prompt de reparación no contiene %q:\n%s", wanted, llm.user)
		}
	}
	if !strings.Contains(string(llm.schema), `"enum":["nombre","precio"]`) {
		t.Errorf("el esquema debe cerrar los nombres de campo: %s", llm.schema)
	}

	for name, reply := range map[string]string{
		"falta un campo":  `{"group": "g1", "next": "none", "fields": [{"name": "nombre", "candidate": "g1.f1", "type": "text", "required": true}]}`,
		"campo no pedido": `{"group": "g1", "next": "none", "fields": [{"name": "nombre", "candidate": "g1.f1", "type": "text", "required": true}, {"name": "extra", "candidate": "g1.f3", "type": "text", "required": true}]}`,
		"campo repetido":  `{"group": "g1", "next": "none", "fields": [{"name": "nombre", "candidate": "g1.f1", "type": "text", "required": true}, {"name": "nombre", "candidate": "g1.f4", "type": "text", "required": true}]}`,
		// Caso real con llama3.2:3b: el enlace "/producto/1" se leería como el precio 1.
		"enlace como precio": `{"group": "g1", "next": "none", "fields": [{"name": "nombre", "candidate": "g1.f1", "type": "text", "required": true}, {"name": "precio", "candidate": "g1.f2", "type": "number", "required": true}]}`,
		"texto como precio":  `{"group": "g1", "next": "none", "fields": [{"name": "nombre", "candidate": "g1.f1", "type": "text", "required": true}, {"name": "precio", "candidate": "g1.f4", "type": "number", "required": true}]}`,
		"precio como nombre": `{"group": "g1", "next": "none", "fields": [{"name": "nombre", "candidate": "g1.f3", "type": "text", "required": true}, {"name": "precio", "candidate": "g1.f3", "type": "number", "required": true}]}`,
	} {
		if _, err := NewProvider(&scriptedLLM{reply: reply}).RepairRecipe(context.Background(), previous, snapshot, failure); err == nil {
			t.Errorf("%s: debería rechazarse", name)
		}
	}
}

func TestFieldName(t *testing.T) {
	for in, want := range map[string]string{
		"Precio Final": "precio_final", " año-de_publicación ": "ano_de_publicacion", "2x": "campo_2x", "¿?": "", "URL": "url",
	} {
		if got := fieldName(in); got != want {
			t.Errorf("fieldName(%q) = %q; se esperaba %q", in, got, want)
		}
	}
}

func TestOllamaCompleteAndStatus(t *testing.T) {
	var chat map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/chat":
			_ = json.NewDecoder(r.Body).Decode(&chat)
			_, _ = w.Write([]byte(`{"message": {"role": "assistant", "content": "{\"group\": \"g1\"}"}, "done": true}`))
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models": [{"name": "mini:latest"}, {"name": "otro:7b"}]}`))
		}
	}))
	defer server.Close()
	ollama := NewOllama(server.URL+"/", "mini", time.Second)

	reply, err := ollama.Complete(context.Background(), "sys", "usr", json.RawMessage(`{"type": "object"}`))
	if err != nil || reply != `{"group": "g1"}` {
		t.Fatalf("reply = %q, err = %v", reply, err)
	}
	messages, _ := chat["messages"].([]any)
	format, _ := chat["format"].(map[string]any)
	options, _ := chat["options"].(map[string]any)
	if chat["model"] != "mini" || chat["stream"] != false || len(messages) != 2 || format["type"] != "object" || options["temperature"] != 0.0 {
		t.Errorf("petición a Ollama = %v", chat)
	}

	status := ollama.Status(context.Background())
	if !status.Enabled || !status.Reachable || !status.ModelInstalled || status.Model != "mini" || len(status.InstalledModels) != 2 || status.Message != "" {
		t.Errorf("estado = %+v", status)
	}
	missing := NewOllama(server.URL, "ausente:1b", time.Second).Status(context.Background())
	if !missing.Reachable || missing.ModelInstalled || !strings.Contains(missing.Message, "ollama pull ausente:1b") {
		t.Errorf("modelo ausente = %+v", missing)
	}
}

func TestOllamaErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("empty") != "" || strings.HasSuffix(r.URL.Path, "/empty/api/chat") {
			_, _ = w.Write([]byte(`{"message": {"content": "  "}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": "model 'x' not found"}`))
	}))
	defer server.Close()

	_, err := NewOllama(server.URL, "x", time.Second).Complete(context.Background(), "s", "u", json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "not found") {
		t.Errorf("error de modelo ausente = %v", err)
	}
	if _, err := NewOllama(server.URL+"/empty", "x", time.Second).Complete(context.Background(), "s", "u", json.RawMessage(`{}`)); err == nil {
		t.Error("una respuesta vacía debe ser un error")
	}

	server.Close()
	down := NewOllama(server.URL, "x", time.Second)
	if _, err := down.Complete(context.Background(), "s", "u", json.RawMessage(`{}`)); err == nil {
		t.Error("sin servidor debe fallar")
	}
	if status := down.Status(context.Background()); status.Reachable || !strings.Contains(status.Message, "ollama serve") {
		t.Errorf("estado sin servidor = %+v", status)
	}
}
