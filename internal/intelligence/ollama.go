package intelligence

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"adaptive-scraper/internal/application"
)

// contextTokens es la ventana de contexto pedida al modelo: suficiente para
// el resumen de una página y pequeña para que quepa en memoria sin GPU.
const contextTokens = 8192

// Ollama es un LLM servido por Ollama a través de su API HTTP.
type Ollama struct {
	baseURL string
	model   string
	client  *http.Client
}

var _ LLM = (*Ollama)(nil)

// NewOllama crea el cliente. timeout limita cada petición de generación.
func NewOllama(baseURL, model string, timeout time.Duration) *Ollama {
	return &Ollama{baseURL: strings.TrimRight(baseURL, "/"), model: model, client: &http.Client{Timeout: timeout}}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Complete usa la salida estructurada de Ollama: format recibe el esquema
// JSON y la generación queda restringida a él.
func (o *Ollama) Complete(ctx context.Context, system, user string, schema json.RawMessage) (string, error) {
	body, err := json.Marshal(map[string]any{
		"model":    o.model,
		"stream":   false,
		"format":   schema,
		"messages": []chatMessage{{Role: "system", Content: system}, {Role: "user", Content: user}},
		"options":  map[string]any{"temperature": 0, "num_ctx": contextTokens},
	})
	if err != nil {
		return "", err
	}
	var response struct {
		Message chatMessage `json:"message"`
	}
	if err := o.do(ctx, o.client, http.MethodPost, "/api/chat", body, &response); err != nil {
		return "", err
	}
	if strings.TrimSpace(response.Message.Content) == "" {
		return "", fmt.Errorf("el modelo %s devolvió una respuesta vacía", o.model)
	}
	return response.Message.Content, nil
}

// Status consulta a Ollama qué modelos tiene instalados.
func (o *Ollama) Status(ctx context.Context) application.AIStatus {
	status := application.AIStatus{Enabled: true, Provider: "ollama", Model: o.model, InstalledModels: []string{}}
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := o.do(ctx, http.DefaultClient, http.MethodGet, "/api/tags", nil, &tags); err != nil {
		status.Message = fmt.Sprintf("No se pudo contactar con Ollama en %s. Inícialo con `ollama serve`.", o.baseURL)
		return status
	}
	status.Reachable = true
	for _, m := range tags.Models {
		status.InstalledModels = append(status.InstalledModels, m.Name)
		// Ollama añade ":latest" a los nombres sin etiqueta.
		if m.Name == o.model || m.Name == o.model+":latest" {
			status.ModelInstalled = true
		}
	}
	if !status.ModelInstalled {
		status.Message = fmt.Sprintf("El modelo %s no está instalado. Descárgalo con `ollama pull %s`.", o.model, o.model)
	}
	return status
}

func (o *Ollama) do(ctx context.Context, client *http.Client, method, path string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, o.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("contactar con Ollama en %s: %w", o.baseURL, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("leer la respuesta de Ollama: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		var apiError struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &apiError)
		if apiError.Error == "" {
			apiError.Error = strings.TrimSpace(string(data))
		}
		return fmt.Errorf("Ollama respondió %d: %s", resp.StatusCode, truncate(apiError.Error, 300))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("respuesta de Ollama ilegible: %w", err)
	}
	return nil
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "…"
}
