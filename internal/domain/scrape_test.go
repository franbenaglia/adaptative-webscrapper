package domain

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseNumber(t *testing.T) {
	tests := map[string]float64{
		"$ 45.999,90":  45999.90,
		"1,234.50 USD": 1234.50,
		"12,5":         12.5,
		"12.5":         12.5,
		"1.234":        1234,
		"1.234.567":    1234567,
		"-3":           -3,
		"Precio: 99.":  99,
		"1 234,5":      1234.5,
	}
	for in, want := range tests {
		got, ok := ParseNumber(in)
		if !ok || got != want {
			t.Errorf("ParseNumber(%q) = %v, %v; se esperaba %v", in, got, ok, want)
		}
	}
	if _, ok := ParseNumber("sin precio"); ok {
		t.Error("un texto sin dígitos no debe convertirse")
	}
}

func TestBuildRecord(t *testing.T) {
	fields := []FieldDefinition{
		{Name: "name", Type: FieldText, Required: true},
		{Name: "price", Type: FieldNumber, Required: true},
		{Name: "stock", Type: FieldText, Required: true},
		{Name: "link", Type: FieldURL},
	}
	raw := RawRecord{"name": "  Teclado \n mecánico ", "price": "consultar", "link": "/p/1"}

	got, missing := BuildRecord(fields, raw, "https://shop.example/list?page=2")

	want := Record{"name": "Teclado mecánico", "price": nil, "stock": nil, "link": "https://shop.example/p/1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("registro = %v; se esperaba %v", got, want)
	}
	if !reflect.DeepEqual(missing, []string{"price", "stock"}) {
		t.Errorf("faltantes = %v", missing)
	}
}

func TestScrapeRequestNormalize(t *testing.T) {
	recipe := validRecipe()

	got, err := ScrapeRequest{Recipe: &recipe}.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != recipe.StartURL || got.MaxPages != DefaultMaxPages || got.MaxRecords != DefaultMaxRecords {
		t.Errorf("valores por defecto incorrectos: %+v", got)
	}

	invalid := map[string]ScrapeRequest{
		"sin receta":          {},
		"host no permitido":   {Recipe: &recipe, URL: "https://evil.example/"},
		"esquema ftp":         {Recipe: &recipe, URL: "ftp://shop.example/"},
		"páginas negativas":   {Recipe: &recipe, MaxPages: -1},
		"registros excesivos": {Recipe: &recipe, MaxRecords: LimitMaxRecords + 1},
	}
	for name, req := range invalid {
		if _, err := req.Normalize(); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("%s: se esperaba ErrInvalidRequest, se obtuvo %v", name, err)
		}
	}

	discover, err := ScrapeRequest{URL: "https://shop.example/list", Instruction: "  extraer nombres  "}.Normalize()
	if err != nil || discover.Recipe != nil || discover.Instruction != "extraer nombres" || discover.MaxPages != DefaultMaxPages {
		t.Errorf("solicitud de descubrimiento = %+v, %v", discover, err)
	}

	bad := recipe
	bad.Fields = nil
	if _, err := (ScrapeRequest{Recipe: &bad}).Normalize(); !errors.Is(err, ErrInvalidRecipe) {
		t.Errorf("se esperaba ErrInvalidRecipe, se obtuvo %v", err)
	}
}

func TestAssessExtraction(t *testing.T) {
	fields := []FieldDefinition{
		{Name: "name", Type: FieldText, Required: true},
		{Name: "price", Type: FieldNumber, Required: true},
		{Name: "note", Type: FieldText},
	}
	ok := []RawRecord{{"name": "A", "price": "1"}, {"name": "B", "price": "2"}, {"name": "C"}}
	if failure := AssessExtraction(fields, ok, "https://x.example/"); failure != nil {
		t.Errorf("un campo requerido ausente en una minoría no es un fallo: %v", failure)
	}

	empty := AssessExtraction(fields, nil, "https://x.example/")
	if empty == nil || empty.Items != 0 || !strings.Contains(empty.Error(), "ningún elemento") {
		t.Errorf("sin elementos = %v", empty)
	}

	broken := []RawRecord{{"name": "A", "price": "consultar"}, {"name": "B"}, {"name": "C", "price": "3"}}
	failure := AssessExtraction(fields, broken, "https://x.example/")
	if failure == nil || failure.Missing["price"] != 2 || len(failure.Missing) != 1 ||
		!strings.Contains(failure.Error(), `"price" falta en 2 de 3`) {
		t.Errorf("campo requerido roto = %v", failure)
	}
}
