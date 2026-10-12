// Package intelligence implementa application.IntelligenceProvider sobre un
// modelo de lenguaje. El modelo nunca escribe selectores ni código: recibe
// candidatos numerados y devuelve identificadores, restringidos además por un
// esquema JSON. Todo lo que devuelve se trata como entrada no confiable.
package intelligence

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"adaptive-scraper/internal/application"
	"adaptive-scraper/internal/domain"
)

// maxResponseBytes limita la respuesta del modelo que se llega a decodificar.
const maxResponseBytes = 64 << 10

const noNext = "none"

// LLM es un modelo capaz de responder con JSON conforme a un esquema.
type LLM interface {
	Complete(ctx context.Context, system, user string, schema json.RawMessage) (string, error)
}

// Provider traduce entre PageSnapshot, el modelo y Recipe.
type Provider struct {
	llm LLM
}

var _ application.IntelligenceProvider = (*Provider)(nil)

func NewProvider(llm LLM) *Provider { return &Provider{llm: llm} }

// selection es la respuesta esperada del modelo.
type selection struct {
	Group  string           `json:"group"`
	Fields []selectionField `json:"fields"`
	Next   string           `json:"next"`
}

type selectionField struct {
	Name      string `json:"name"`
	Candidate string `json:"candidate"`
	Type      string `json:"type"`
	Required  bool   `json:"required"`
}

const systemPrompt = `You configure a web scraper. A page has been analysed into numbered CANDIDATES.
Your job is to choose which candidates hold the data the user asks for.

Rules:
- Choose exactly one GROUP: the repeated items that represent the records the user wants.
- For each piece of data requested, choose one field candidate from THAT group, judging by its samples.
- Never choose two candidates for the same data. Do not add data the user did not ask for.
- "type" is "number" for prices and quantities, "url" for links and images, otherwise "text".
- "required" is true for data every record must have.
- "next" is the id of the control that leads to the next page of results, or "none".
- Reply with JSON only.`

// DiscoverRecipe pide al modelo que relacione la instrucción con los candidatos.
func (p *Provider) DiscoverRecipe(ctx context.Context, snapshot domain.PageSnapshot, instruction string) (domain.Recipe, error) {
	user := fmt.Sprintf("USER REQUEST: %s\n\nName each field in snake_case, in the language of the request.\n\n%s",
		instruction, describe(snapshot))
	chosen, err := p.ask(ctx, user, schemaFor(snapshot, nil))
	if err != nil {
		return domain.Recipe{}, err
	}
	return buildRecipe(snapshot, chosen, nil)
}

// RepairRecipe pide al modelo que vuelva a localizar los mismos campos.
func (p *Provider) RepairRecipe(ctx context.Context, previous domain.Recipe, snapshot domain.PageSnapshot, failure domain.ExtractionFailure) (domain.Recipe, error) {
	var wanted strings.Builder
	for _, f := range previous.Fields {
		fmt.Fprintf(&wanted, "- %s (%s%s)\n", f.Name, f.Type, map[bool]string{true: ", required", false: ""}[f.Required])
	}
	user := fmt.Sprintf(`The page layout changed and the scraper stopped working (%s).
Locate again exactly these fields, keeping the same names:
%s
%s`, failure.Error(), wanted.String(), describe(snapshot))
	chosen, err := p.ask(ctx, user, schemaFor(snapshot, previous.Fields))
	if err != nil {
		return domain.Recipe{}, err
	}
	return buildRecipe(snapshot, chosen, previous.Fields)
}

func (p *Provider) ask(ctx context.Context, user string, schema json.RawMessage) (selection, error) {
	var chosen selection
	raw, err := p.llm.Complete(ctx, systemPrompt, user, schema)
	if err != nil {
		return chosen, err
	}
	if len(raw) > maxResponseBytes {
		return chosen, fmt.Errorf("la respuesta del modelo supera los %d bytes", maxResponseBytes)
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&chosen); err != nil {
		return chosen, fmt.Errorf("la respuesta del modelo no es el JSON esperado: %w", err)
	}
	if dec.More() {
		return chosen, errors.New("la respuesta del modelo contiene más de un objeto JSON")
	}
	return chosen, nil
}

// describe lista los candidatos en texto compacto.
func describe(snapshot domain.PageSnapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "PAGE: %s\nTITLE: %s\n\nCANDIDATES\n", snapshot.URL, snapshot.Title)
	for _, g := range snapshot.Groups {
		fmt.Fprintf(&b, "\nGROUP %s: %d repeated items\n", g.ID, g.Count)
		for _, f := range g.Fields {
			kind := "text"
			if f.Attribute != "" {
				kind = "attribute " + f.Attribute
			}
			quoted := make([]string, len(f.Samples))
			for i, s := range f.Samples {
				quoted[i] = fmt.Sprintf("%q", s)
			}
			fmt.Fprintf(&b, "  %s (%s) samples: %s\n", f.ID, kind, strings.Join(quoted, " | "))
		}
	}
	b.WriteString("\nNEXT PAGE CONTROLS\n")
	if len(snapshot.Next) == 0 {
		b.WriteString("  (none found)\n")
	}
	for _, n := range snapshot.Next {
		fmt.Fprintf(&b, "  %s: %q\n", n.ID, n.Text)
	}
	return b.String()
}

// schemaFor construye el esquema JSON de la respuesta. Los identificadores
// (y, al reparar, los nombres de campo) son enumeraciones cerradas, de modo
// que un proveedor con salida estructurada no puede inventarlos.
func schemaFor(snapshot domain.PageSnapshot, keep []domain.FieldDefinition) json.RawMessage {
	groups, candidates, next := []string{}, []string{}, []string{noNext}
	for _, g := range snapshot.Groups {
		groups = append(groups, g.ID)
		for _, f := range g.Fields {
			candidates = append(candidates, f.ID)
		}
	}
	for _, n := range snapshot.Next {
		next = append(next, n.ID)
	}
	name := map[string]any{"type": "string"}
	if len(keep) > 0 {
		names := make([]string, len(keep))
		for i, f := range keep {
			names[i] = f.Name
		}
		name["enum"] = names
	}
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"group": map[string]any{"type": "string", "enum": groups},
			"fields": map[string]any{
				"type": "array", "minItems": 1, "maxItems": domain.MaxFields,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":      name,
						"candidate": map[string]any{"type": "string", "enum": candidates},
						"type":      map[string]any{"type": "string", "enum": []string{domain.FieldText, domain.FieldNumber, domain.FieldURL}},
						"required":  map[string]any{"type": "boolean"},
					},
					"required": []string{"name", "candidate", "type", "required"},
				},
			},
			"next": map[string]any{"type": "string", "enum": next},
		},
		"required": []string{"group", "fields", "next"},
	}
	raw, _ := json.Marshal(schema)
	return raw
}

// buildRecipe convierte la elección del modelo en una receta, comprobando
// cada identificador contra el snapshot. Con keep (reparación) exige
// exactamente esos campos y conserva su tipo y obligatoriedad.
func buildRecipe(snapshot domain.PageSnapshot, chosen selection, keep []domain.FieldDefinition) (domain.Recipe, error) {
	group, ok := snapshot.Group(chosen.Group)
	if !ok {
		return domain.Recipe{}, fmt.Errorf("el modelo eligió un grupo inexistente %q", chosen.Group)
	}
	if len(chosen.Fields) == 0 {
		return domain.Recipe{}, errors.New("el modelo no eligió ningún campo")
	}
	if len(chosen.Fields) > domain.MaxFields {
		return domain.Recipe{}, fmt.Errorf("el modelo eligió %d campos; el máximo es %d", len(chosen.Fields), domain.MaxFields)
	}
	previous := map[string]domain.FieldDefinition{}
	for _, f := range keep {
		previous[f.Name] = f
	}

	recipe := domain.Recipe{ItemLocator: group.Selector, Pagination: domain.PaginationRule{Type: domain.PaginationNone}}
	used := map[string]bool{}
	for _, f := range chosen.Fields {
		candidate, ok := group.Field(f.Candidate)
		if !ok {
			return domain.Recipe{}, fmt.Errorf("el modelo eligió el campo %q, que no pertenece al grupo %s", f.Candidate, group.ID)
		}
		field := domain.FieldDefinition{
			Name: fieldName(f.Name), Type: f.Type, Locator: candidate.Selector, Attribute: candidate.Attribute, Required: f.Required,
		}
		if len(keep) > 0 {
			old, ok := previous[f.Name]
			if !ok {
				return domain.Recipe{}, fmt.Errorf("el modelo devolvió un campo no pedido %q", f.Name)
			}
			field.Name, field.Type, field.Required = old.Name, old.Type, old.Required
		} else {
			field.Type = fieldType(f.Type, candidate)
		}
		if field.Name == "" || used[field.Name] {
			if len(keep) > 0 {
				return domain.Recipe{}, fmt.Errorf("el modelo repitió el campo %q", f.Name)
			}
			continue // nombre vacío o repetido: se queda la primera elección
		}
		used[field.Name] = true
		recipe.Fields = append(recipe.Fields, field)
	}
	if len(recipe.Fields) == 0 {
		return domain.Recipe{}, errors.New("el modelo no devolvió ningún campo con nombre válido")
	}
	for _, f := range keep {
		if !used[f.Name] {
			return domain.Recipe{}, fmt.Errorf("el modelo no localizó el campo %q", f.Name)
		}
	}

	if chosen.Next != "" && chosen.Next != noNext {
		found := false
		for _, n := range snapshot.Next {
			if n.ID == chosen.Next {
				recipe.Pagination = domain.PaginationRule{Type: domain.PaginationNextButton, NextLocator: n.Selector}
				found = true
			}
		}
		if !found {
			return domain.Recipe{}, fmt.Errorf("el modelo eligió un control de paginación inexistente %q", chosen.Next)
		}
	}
	return recipe, nil
}

// fieldType corrige el tipo propuesto con lo que muestran los datos: un
// atributo de enlace es una URL y solo es número lo que se puede leer como tal.
func fieldType(proposed string, candidate domain.CandidateField) string {
	switch candidate.Attribute {
	case "href", "src":
		return domain.FieldURL
	}
	if proposed == domain.FieldNumber {
		for _, sample := range candidate.Samples {
			if _, ok := domain.ParseNumber(sample); !ok {
				return domain.FieldText
			}
		}
		return domain.FieldNumber
	}
	if proposed == domain.FieldURL {
		return domain.FieldText // texto visible, no un enlace
	}
	return domain.FieldText
}

var (
	accents      = strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n", "ç", "c")
	notNameChars = regexp.MustCompile(`[^a-z0-9_]+`)
)

// fieldName normaliza el nombre propuesto a snake_case ASCII.
func fieldName(name string) string {
	name = accents.Replace(strings.ToLower(strings.TrimSpace(name)))
	name = strings.Trim(notNameChars.ReplaceAllString(name, "_"), "_")
	if name == "" {
		return ""
	}
	if name[0] >= '0' && name[0] <= '9' {
		name = "campo_" + name
	}
	if len(name) > 64 {
		name = name[:64]
	}
	return name
}
