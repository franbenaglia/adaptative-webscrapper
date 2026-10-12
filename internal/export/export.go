// Package export escribe resultados en formatos de intercambio.
package export

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"adaptive-scraper/internal/domain"
)

const (
	FormatJSON = "json"
	FormatCSV  = "csv"
)

// Exporter escribe los registros respetando el orden de fields.
type Exporter interface {
	Write(w io.Writer, fields []domain.FieldDefinition, records []domain.Record) error
}

// ForFormat devuelve el exportador de un formato.
func ForFormat(format string) (Exporter, error) {
	switch strings.ToLower(format) {
	case FormatJSON:
		return JSON{}, nil
	case FormatCSV:
		return CSV{}, nil
	default:
		return nil, fmt.Errorf("formato de exportación no admitido %q (use json o csv)", format)
	}
}

// FormatFromPath deduce el formato de la extensión; JSON si no la reconoce.
func FormatFromPath(path string) string {
	if strings.EqualFold(filepath.Ext(path), ".csv") {
		return FormatCSV
	}
	return FormatJSON
}

// JSON escribe un array de objetos con las claves en el orden de la receta.
type JSON struct{}

func (JSON) Write(w io.Writer, fields []domain.FieldDefinition, records []domain.Record) error {
	var buf bytes.Buffer
	buf.WriteString("[")
	for i, record := range records {
		if i > 0 {
			buf.WriteString(",")
		}
		buf.WriteString("\n  {")
		for j, f := range fields {
			if j > 0 {
				buf.WriteString(", ")
			}
			key, _ := json.Marshal(f.Name)
			value, err := json.Marshal(record[f.Name])
			if err != nil {
				return fmt.Errorf("registro %d, campo %q: %w", i, f.Name, err)
			}
			buf.Write(key)
			buf.WriteString(": ")
			buf.Write(value)
		}
		buf.WriteString("}")
	}
	if len(records) > 0 {
		buf.WriteString("\n")
	}
	buf.WriteString("]\n")
	_, err := w.Write(buf.Bytes())
	return err
}

// CSV escribe una cabecera con los nombres de campo y una fila por registro.
// Los valores nil quedan como celda vacía.
type CSV struct{}

func (CSV) Write(w io.Writer, fields []domain.FieldDefinition, records []domain.Record) error {
	cw := csv.NewWriter(w)
	row := make([]string, len(fields))
	for i, f := range fields {
		row[i] = f.Name
	}
	if err := cw.Write(row); err != nil {
		return err
	}
	for _, record := range records {
		for i, f := range fields {
			row[i] = cell(record[f.Name])
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func cell(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return fmt.Sprint(t)
	}
}
