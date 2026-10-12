package domain

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Valores por defecto y topes de una ejecución.
const (
	DefaultMaxPages   = 5
	DefaultMaxRecords = 100
	LimitMaxPages     = 1000
	LimitMaxRecords   = 100000
)

// ScrapeRequest describe una ejecución. URL vacía significa Recipe.StartURL.
// En la Fase 1 la receta siempre llega en línea; RecipeID e Instruction se
// añadirán con la persistencia y la IA.
type ScrapeRequest struct {
	URL        string
	Recipe     *Recipe
	MaxPages   int
	MaxRecords int
	Headless   bool
}

// ScrapeResult es el resultado de una ejecución.
type ScrapeResult struct {
	Fields       []FieldDefinition
	Records      []Record
	PagesVisited int
	Warnings     []string
}

// Normalize valida la solicitud y aplica valores por defecto.
func (r ScrapeRequest) Normalize() (ScrapeRequest, error) {
	if r.Recipe == nil {
		return r, fmt.Errorf("%w: falta la receta", ErrInvalidRequest)
	}
	if err := r.Recipe.Validate(); err != nil {
		return r, err
	}
	if r.URL == "" {
		r.URL = r.Recipe.StartURL
	}
	u, err := ParseHTTPURL(r.URL)
	if err != nil {
		return r, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	if !r.Recipe.AllowsHost(u.Hostname()) {
		return r, fmt.Errorf("%w: el host %q no está permitido por la receta", ErrInvalidRequest, u.Hostname())
	}
	if r.MaxPages == 0 {
		r.MaxPages = DefaultMaxPages
	}
	if r.MaxRecords == 0 {
		r.MaxRecords = DefaultMaxRecords
	}
	if r.MaxPages < 0 || r.MaxPages > LimitMaxPages {
		return r, fmt.Errorf("%w: max-pages debe estar entre 1 y %d", ErrInvalidRequest, LimitMaxPages)
	}
	if r.MaxRecords < 0 || r.MaxRecords > LimitMaxRecords {
		return r, fmt.Errorf("%w: max-records debe estar entre 1 y %d", ErrInvalidRequest, LimitMaxRecords)
	}
	return r, nil
}

// RawRecord es lo que devuelve el navegador para un elemento: solo los campos
// encontrados, como texto sin convertir.
type RawRecord map[string]string

// BuildRecord convierte un RawRecord a los tipos de la receta. Los campos
// ausentes o no convertibles quedan en nil; missing lista los requeridos que
// faltan. pageURL resuelve las URL relativas.
func BuildRecord(fields []FieldDefinition, raw RawRecord, pageURL string) (rec Record, missing []string) {
	rec = make(Record, len(fields))
	for _, f := range fields {
		rec[f.Name] = nil
		if v, ok := raw[f.Name]; ok {
			rec[f.Name] = convert(f.Type, normalizeSpace(v), pageURL)
		}
		if f.Required && rec[f.Name] == nil {
			missing = append(missing, f.Name)
		}
	}
	return rec, missing
}

func convert(fieldType, v, pageURL string) any {
	if v == "" {
		return nil
	}
	switch fieldType {
	case FieldNumber:
		if n, ok := ParseNumber(v); ok {
			return n
		}
		return nil
	case FieldURL:
		base, err := url.Parse(pageURL)
		if err != nil {
			return v
		}
		ref, err := url.Parse(v)
		if err != nil {
			return nil
		}
		return base.ResolveReference(ref).String()
	default:
		return v
	}
}

var numberRe = regexp.MustCompile(`-?\d[\d.,\s\x{00a0}]*`)

// ParseNumber extrae el primer número de un texto como "$ 1.234,50" o
// "1,234.50 USD". Con un único separador seguido de exactamente tres dígitos
// lo trata como separador de miles ("1.234" -> 1234).
func ParseNumber(s string) (float64, bool) {
	m := numberRe.FindString(s)
	m = strings.NewReplacer(" ", "", " ", "").Replace(m)
	m = strings.TrimRight(m, ".,")
	if m == "" {
		return 0, false
	}
	lastDot, lastComma := strings.LastIndex(m, "."), strings.LastIndex(m, ",")
	switch {
	case lastDot >= 0 && lastComma >= 0:
		if lastComma > lastDot {
			m = strings.ReplaceAll(m, ".", "")
			m = strings.Replace(m, ",", ".", 1)
		} else {
			m = strings.ReplaceAll(m, ",", "")
		}
	case lastComma >= 0:
		m = normalizeSingleSeparator(m, ",")
	case lastDot >= 0:
		m = normalizeSingleSeparator(m, ".")
	}
	n, err := strconv.ParseFloat(m, 64)
	return n, err == nil
}

func normalizeSingleSeparator(m, sep string) string {
	last := strings.LastIndex(m, sep)
	if strings.Count(m, sep) > 1 || len(m)-last-1 == 3 {
		return strings.ReplaceAll(m, sep, "")
	}
	return strings.Replace(m, sep, ".", 1)
}

func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
