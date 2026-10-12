package domain

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Valores por defecto y topes de una ejecución.
const (
	DefaultMaxPages   = 5
	DefaultMaxRecords = 100
	LimitMaxPages     = 1000
	LimitMaxRecords   = 100000
)

// MaxInstructionLength limita el texto libre que se envía al modelo.
const MaxInstructionLength = 2000

// ScrapeRequest describe una ejecución. Con Recipe se ejecuta esa receta (URL
// vacía significa Recipe.StartURL). Sin Recipe hacen falta URL e Instruction:
// la receta se descubre con IA.
type ScrapeRequest struct {
	URL         string
	Instruction string
	Recipe      *Recipe
	MaxPages    int
	MaxRecords  int
	Headless    bool
	// DiagnosticsDir, si no está vacío, es donde guardar una captura y una
	// traza cuando la ejecución falla o queda bloqueada.
	DiagnosticsDir string
}

// Orígenes de la receta con la que terminó una ejecución.
const (
	RecipeProvided   = "provided"
	RecipeDiscovered = "discovered"
	RecipeRepaired   = "repaired"
)

// ScrapeResult es el resultado de una ejecución. Recipe es la receta
// finalmente usada (nil si no llegó a haber ninguna) y RecipeOrigin indica
// si es la recibida, una descubierta o una reparada.
type ScrapeResult struct {
	Fields       []FieldDefinition
	Records      []Record
	PagesVisited int
	Warnings     []string
	Recipe       *Recipe
	RecipeOrigin string
}

// Normalize valida la solicitud y aplica valores por defecto.
func (r ScrapeRequest) Normalize() (ScrapeRequest, error) {
	r.Instruction = strings.TrimSpace(r.Instruction)
	if len(r.Instruction) > MaxInstructionLength {
		return r, fmt.Errorf("%w: la instrucción supera los %d caracteres", ErrInvalidRequest, MaxInstructionLength)
	}
	if r.Recipe == nil {
		if r.Instruction == "" || r.URL == "" {
			return r, fmt.Errorf("%w: indique una receta, o una URL y una instrucción para descubrirla", ErrInvalidRequest)
		}
		if _, err := ParseHTTPURL(r.URL); err != nil {
			return r, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
		}
	} else {
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

// LooksNumeric indica si un texto es esencialmente un número, con o sin
// símbolo o unidad ("$ 1.234,50", "12 kg"). ParseNumber es más permisivo:
// también saca un número de "/producto/1", que no lo es.
func LooksNumeric(s string) bool {
	if _, ok := ParseNumber(s); !ok {
		return false
	}
	digits, letters := 0, 0
	for _, r := range s {
		switch {
		case unicode.IsDigit(r):
			digits++
		case unicode.IsLetter(r):
			letters++
		}
	}
	return letters <= digits
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
