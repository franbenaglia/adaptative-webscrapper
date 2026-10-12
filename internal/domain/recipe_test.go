package domain

import (
	"errors"
	"strings"
	"testing"
)

func validRecipe() Recipe {
	return Recipe{
		Name:        "demo",
		StartURL:    "https://shop.example/products",
		ItemLocator: ".product",
		Fields: []FieldDefinition{
			{Name: "name", Type: FieldText, Locator: ".name", Required: true},
			{Name: "price", Type: FieldNumber, Locator: ".price"},
		},
		Pagination: PaginationRule{Type: PaginationNextButton, NextLocator: "a.next"},
	}
}

func TestRecipeValidate(t *testing.T) {
	if err := validRecipe().Validate(); err != nil {
		t.Fatalf("receta válida rechazada: %v", err)
	}

	tests := map[string]func(*Recipe){
		"sin nombre":             func(r *Recipe) { r.Name = " " },
		"esquema file":           func(r *Recipe) { r.StartURL = "file:///etc/passwd" },
		"esquema javascript":     func(r *Recipe) { r.StartURL = "javascript:alert(1)" },
		"url sin host":           func(r *Recipe) { r.StartURL = "https:///x" },
		"host fuera de la lista": func(r *Recipe) { r.AllowedHosts = []string{"other.example"} },
		"host permitido con ruta": func(r *Recipe) {
			r.AllowedHosts = []string{"shop.example/x"}
		},
		"sin item_locator":    func(r *Recipe) { r.ItemLocator = "" },
		"sin campos":          func(r *Recipe) { r.Fields = nil },
		"campo sin nombre":    func(r *Recipe) { r.Fields[0].Name = "" },
		"nombre con espacio":  func(r *Recipe) { r.Fields[0].Name = "the name" },
		"campo duplicado":     func(r *Recipe) { r.Fields[1].Name = "name" },
		"tipo desconocido":    func(r *Recipe) { r.Fields[0].Type = "script" },
		"selector largo":      func(r *Recipe) { r.Fields[0].Locator = strings.Repeat("a", MaxSelectorLength+1) },
		"motor xpath":         func(r *Recipe) { r.Fields[0].Locator = "xpath=//div" },
		"selector encadenado": func(r *Recipe) { r.ItemLocator = "div >> text=x" },
		"selector con script": func(r *Recipe) { r.ItemLocator = "a[href^='javascript:']" },
		"atributo inválido":   func(r *Recipe) { r.Fields[0].Attribute = "on click" },
		"demasiados campos": func(r *Recipe) {
			r.Fields = nil
			for i := 0; i <= MaxFields; i++ {
				r.Fields = append(r.Fields, FieldDefinition{Name: "f" + strings.Repeat("x", i), Type: FieldText})
			}
		},
		"paginación desconocida":     func(r *Recipe) { r.Pagination.Type = "infinite_scroll" },
		"next_button sin selector":   func(r *Recipe) { r.Pagination.NextLocator = "" },
		"none con selector sobrante": func(r *Recipe) { r.Pagination.Type = PaginationNone },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			r := validRecipe()
			mutate(&r)
			if err := r.Validate(); !errors.Is(err, ErrInvalidRecipe) {
				t.Fatalf("se esperaba ErrInvalidRecipe, se obtuvo %v", err)
			}
		})
	}
}

func TestRecipeAllowsHost(t *testing.T) {
	r := validRecipe()
	if !r.AllowsHost("SHOP.example") || r.AllowsHost("evil.example") {
		t.Fatal("sin allowed_hosts solo debe permitirse el host de start_url")
	}
	r.AllowedHosts = []string{"shop.example", "cdn.example"}
	if !r.AllowsHost("cdn.example") || r.AllowsHost("evil.example") {
		t.Fatal("allowed_hosts no se respeta")
	}
}
