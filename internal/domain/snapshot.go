package domain

import (
	"fmt"
	"sort"
	"strings"
)

// PageSnapshot es la representación reducida de una página que se envía al
// modelo. No contiene HTML: solo candidatos calculados de forma determinista,
// cada uno con un identificador. El modelo elige identificadores; los
// selectores nunca salen de él.
type PageSnapshot struct {
	URL    string           `json:"url"`
	Title  string           `json:"title"`
	Text   string           `json:"text"`
	Groups []CandidateGroup `json:"groups"`
	Next   []NextCandidate  `json:"next"`
}

// CandidateGroup es un conjunto de elementos repetidos (tarjetas, filas...).
type CandidateGroup struct {
	ID       string           `json:"id"`
	Selector string           `json:"selector"`
	Count    int              `json:"count"`
	Fields   []CandidateField `json:"fields"`
}

// CandidateField es un dato localizable dentro de cada elemento del grupo.
type CandidateField struct {
	ID        string   `json:"id"`
	Selector  string   `json:"selector"`
	Attribute string   `json:"attribute,omitempty"`
	Samples   []string `json:"samples"`
	// Coverage es en cuántos de los elementos inspeccionados aparece.
	Coverage int `json:"coverage"`
}

// NextCandidate es un control que podría llevar a la página siguiente.
type NextCandidate struct {
	ID       string `json:"id"`
	Selector string `json:"selector"`
	Text     string `json:"text"`
}

// Group busca un grupo por identificador.
func (s PageSnapshot) Group(id string) (CandidateGroup, bool) {
	for _, g := range s.Groups {
		if g.ID == id {
			return g, true
		}
	}
	return CandidateGroup{}, false
}

// Field busca un campo candidato por identificador.
func (g CandidateGroup) Field(id string) (CandidateField, bool) {
	for _, f := range g.Fields {
		if f.ID == id {
			return f, true
		}
	}
	return CandidateField{}, false
}

// MaxMissingRequiredRatio es la fracción de elementos a partir de la cual la
// ausencia de un campo requerido indica que la receta ya no encaja.
const MaxMissingRequiredRatio = 0.5

// ExtractionFailure describe por qué una receta no funciona en una página.
type ExtractionFailure struct {
	Items   int
	Missing map[string]int // campo requerido -> elementos donde falta
}

func (f ExtractionFailure) Error() string {
	if f.Items == 0 {
		return "ningún elemento coincide con el selector de elementos"
	}
	names := make([]string, 0, len(f.Missing))
	for name := range f.Missing {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, name := range names {
		parts[i] = fmt.Sprintf("%q falta en %d de %d elementos", name, f.Missing[name], f.Items)
	}
	return "campos requeridos ausentes: " + strings.Join(parts, ", ")
}

// AssessExtraction decide si la receta funciona sobre lo extraído de una
// página. Devuelve nil cuando funciona. Falla si no hay elementos o si algún
// campo requerido falta (o no se puede convertir) en más de la mitad.
func AssessExtraction(fields []FieldDefinition, raws []RawRecord, pageURL string) *ExtractionFailure {
	failure := ExtractionFailure{Items: len(raws), Missing: map[string]int{}}
	if len(raws) == 0 {
		return &failure
	}
	counts := map[string]int{}
	for _, raw := range raws {
		_, missing := BuildRecord(fields, raw, pageURL)
		for _, name := range missing {
			counts[name]++
		}
	}
	for name, n := range counts {
		if float64(n) > MaxMissingRequiredRatio*float64(len(raws)) {
			failure.Missing[name] = n
		}
	}
	if len(failure.Missing) == 0 {
		return nil
	}
	return &failure
}
