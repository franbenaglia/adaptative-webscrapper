// Package domain contiene los tipos y reglas del scraper. No depende de
// HTTP, Cobra, Playwright, SQLite ni Ollama.
package domain

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Errores centinela que las capas externas traducen a estados o códigos de salida.
var (
	ErrInvalidRecipe  = errors.New("receta inválida")
	ErrInvalidRequest = errors.New("solicitud inválida")
	ErrBlocked        = errors.New("acceso bloqueado")
)

// Tipos de campo admitidos.
const (
	FieldText   = "text"
	FieldNumber = "number"
	FieldURL    = "url"
)

// Tipos de paginación admitidos.
const (
	PaginationNone       = "none"
	PaginationNextButton = "next_button"
)

// Límites aplicados a cualquier receta, venga de un archivo o de un modelo.
const (
	MaxFields         = 50
	MaxSelectorLength = 500
)

// Record es un registro extraído: nombre de campo -> valor (string, float64 o nil).
type Record map[string]any

// FieldDefinition describe cómo obtener un campo dentro de cada elemento.
// Locator es un selector CSS relativo al elemento; vacío significa el propio
// elemento. Attribute vacío significa el texto visible.
type FieldDefinition struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Locator   string `json:"locator"`
	Attribute string `json:"attribute,omitempty"`
	Required  bool   `json:"required,omitempty"`
}

// PaginationRule describe cómo pasar a la página siguiente.
type PaginationRule struct {
	Type        string `json:"type"`
	NextLocator string `json:"next_locator,omitempty"`
}

// Recipe es una descripción declarativa de una extracción. Nunca contiene código.
type Recipe struct {
	ID           string            `json:"id,omitempty"`
	Name         string            `json:"name"`
	StartURL     string            `json:"start_url"`
	AllowedHosts []string          `json:"allowed_hosts,omitempty"`
	ItemLocator  string            `json:"item_locator"`
	Fields       []FieldDefinition `json:"fields"`
	Pagination   PaginationRule    `json:"pagination"`
	Version      int               `json:"version,omitempty"`
}

var (
	fieldNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
	attributeRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_:.-]{0,63}$`)
	// Prefijos de motores de selección de Playwright (xpath=, text=, css=...).
	selectorEngineRe = regexp.MustCompile(`^\s*[A-Za-z_:-]+\s*=`)
)

// Validate comprueba la receta completa. Todos los errores envuelven ErrInvalidRecipe.
func (r Recipe) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return invalidRecipe("falta el nombre")
	}
	start, err := ParseHTTPURL(r.StartURL)
	if err != nil {
		return invalidRecipe("start_url: %v", err)
	}
	for _, h := range r.AllowedHosts {
		if h == "" || strings.ContainsAny(h, "/ \t") {
			return invalidRecipe("host permitido inválido %q", h)
		}
	}
	if !r.AllowsHost(start.Hostname()) {
		return invalidRecipe("el host de start_url %q no está en allowed_hosts", start.Hostname())
	}
	if err := validateSelector("item_locator", r.ItemLocator, false); err != nil {
		return err
	}
	if len(r.Fields) == 0 {
		return invalidRecipe("no define campos")
	}
	if len(r.Fields) > MaxFields {
		return invalidRecipe("define %d campos; el máximo es %d", len(r.Fields), MaxFields)
	}
	seen := make(map[string]bool, len(r.Fields))
	for _, f := range r.Fields {
		if !fieldNameRe.MatchString(f.Name) {
			return invalidRecipe("nombre de campo inválido %q", f.Name)
		}
		if seen[f.Name] {
			return invalidRecipe("campo duplicado %q", f.Name)
		}
		seen[f.Name] = true
		switch f.Type {
		case FieldText, FieldNumber, FieldURL:
		default:
			return invalidRecipe("campo %q: tipo no admitido %q", f.Name, f.Type)
		}
		if err := validateSelector("campo "+f.Name, f.Locator, true); err != nil {
			return err
		}
		if f.Attribute != "" && !attributeRe.MatchString(f.Attribute) {
			return invalidRecipe("campo %q: atributo inválido %q", f.Name, f.Attribute)
		}
	}
	switch r.Pagination.Type {
	case "", PaginationNone:
		if r.Pagination.NextLocator != "" {
			return invalidRecipe("paginación %q no admite next_locator", PaginationNone)
		}
	case PaginationNextButton:
		if err := validateSelector("pagination.next_locator", r.Pagination.NextLocator, false); err != nil {
			return err
		}
	default:
		return invalidRecipe("tipo de paginación no admitido %q", r.Pagination.Type)
	}
	return nil
}

// AllowsHost indica si la receta puede navegar al host. Sin AllowedHosts
// explícitos solo se permite el host de StartURL.
func (r Recipe) AllowsHost(host string) bool {
	host = strings.ToLower(host)
	if len(r.AllowedHosts) == 0 {
		start, err := url.Parse(r.StartURL)
		return err == nil && strings.ToLower(start.Hostname()) == host
	}
	for _, h := range r.AllowedHosts {
		if strings.ToLower(h) == host {
			return true
		}
	}
	return false
}

// HasPagination indica si la receta sabe pasar de página.
func (r Recipe) HasPagination() bool {
	return r.Pagination.Type == PaginationNextButton
}

// ParseHTTPURL acepta únicamente URL absolutas http o https con host.
func ParseHTTPURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("URL mal formada: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("esquema no admitido %q: solo http y https", u.Scheme)
	}
	if u.Hostname() == "" {
		return nil, errors.New("la URL no tiene host")
	}
	return u, nil
}

// validateSelector admite solo CSS plano: sin motores de Playwright ni
// encadenado ">>", que permitirían más de lo que una receta necesita.
func validateSelector(where, sel string, allowEmpty bool) error {
	if strings.TrimSpace(sel) == "" {
		if allowEmpty {
			return nil
		}
		return invalidRecipe("%s: selector vacío", where)
	}
	if len(sel) > MaxSelectorLength {
		return invalidRecipe("%s: selector de más de %d caracteres", where, MaxSelectorLength)
	}
	lower := strings.ToLower(sel)
	if strings.Contains(sel, ">>") || selectorEngineRe.MatchString(sel) ||
		strings.Contains(lower, "javascript:") || strings.Contains(lower, "<script") {
		return invalidRecipe("%s: solo se admiten selectores CSS simples", where)
	}
	return nil
}

func invalidRecipe(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidRecipe, fmt.Sprintf(format, args...))
}
