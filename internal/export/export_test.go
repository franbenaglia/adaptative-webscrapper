package export

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"

	"adaptive-scraper/internal/domain"
)

var (
	fields = []domain.FieldDefinition{{Name: "name"}, {Name: "price"}, {Name: "stock"}}
	sample = []domain.Record{
		{"name": `Teclado "Pro", negro`, "price": 45999.9, "stock": "En stock"},
		{"name": "Webcam", "price": 22000.0, "stock": nil},
	}
)

func TestJSONKeepsFieldOrderAndNulls(t *testing.T) {
	var buf bytes.Buffer
	if err := (JSON{}).Write(&buf, fields, sample); err != nil {
		t.Fatal(err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, buf.String())
	}
	if len(decoded) != 2 || decoded[0]["price"] != 45999.9 || decoded[1]["stock"] != nil {
		t.Errorf("contenido = %v", decoded)
	}
	first := strings.Split(buf.String(), "\n")[1]
	if strings.Index(first, `"name"`) > strings.Index(first, `"price"`) || strings.Index(first, `"price"`) > strings.Index(first, `"stock"`) {
		t.Errorf("orden de claves incorrecto: %s", first)
	}
}

func TestJSONEmptyIsValidArray(t *testing.T) {
	var buf bytes.Buffer
	if err := (JSON{}).Write(&buf, fields, nil); err != nil || strings.TrimSpace(buf.String()) != "[]" {
		t.Fatalf("salida = %q err = %v", buf.String(), err)
	}
}

// CSV y JSON deben contener los mismos datos.
func TestCSVMatchesJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := (CSV{}).Write(&buf, fields, sample); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"name", "price", "stock"},
		{`Teclado "Pro", negro`, "45999.9", "En stock"},
		{"Webcam", "22000", ""},
	}
	if len(rows) != len(want) {
		t.Fatalf("filas = %v", rows)
	}
	for i := range want {
		if strings.Join(rows[i], "|") != strings.Join(want[i], "|") {
			t.Errorf("fila %d = %v; se esperaba %v", i, rows[i], want[i])
		}
	}
}

func TestForFormatAndFormatFromPath(t *testing.T) {
	if _, err := ForFormat("xml"); err == nil {
		t.Error("xml debería rechazarse")
	}
	if e, _ := ForFormat("CSV"); e != (CSV{}) {
		t.Error("ForFormat debe ignorar mayúsculas")
	}
	for path, want := range map[string]string{"a.csv": FormatCSV, "a.CSV": FormatCSV, "a.json": FormatJSON, "": FormatJSON} {
		if got := FormatFromPath(path); got != want {
			t.Errorf("FormatFromPath(%q) = %q", path, got)
		}
	}
}
