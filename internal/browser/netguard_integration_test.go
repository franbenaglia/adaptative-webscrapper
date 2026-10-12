//go:build browser

package browser

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"adaptive-scraper/internal/application"
	"adaptive-scraper/internal/domain"
	"adaptive-scraper/internal/netguard"
)

func openWith(t *testing.T, options Options, browserOptions application.BrowserOptions) application.BrowserSession {
	t.Helper()
	launcher := NewLauncher(options)
	t.Cleanup(func() { _ = launcher.Close() })
	browserOptions.Headless, browserOptions.NavigationTimeout, browserOptions.ActionTimeout = true, 30*time.Second, 10*time.Second
	session, err := launcher.Open(context.Background(), browserOptions)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// Con la política por defecto el navegador no llega a servicios locales.
func TestDefaultPolicyBlocksLocalServices(t *testing.T) {
	var hits atomic.Int32
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("<html><body>panel interno</body></html>"))
	}))
	defer internal.Close()
	session := openWith(t, Options{}, application.BrowserOptions{})

	for _, target := range []string{internal.URL, strings.Replace(internal.URL, "127.0.0.1", "localhost", 1)} {
		if err := session.Navigate(context.Background(), target); !errors.Is(err, netguard.ErrForbidden) {
			t.Errorf("navegar a %s = %v", target, err)
		}
	}
	if hits.Load() != 0 {
		t.Errorf("el servicio interno recibió %d peticiones", hits.Load())
	}
}

// Una página permitida no puede alcanzar un destino prohibido ni con una
// redirección ni con un subrecurso: eso lo hace cumplir el proxy.
func TestProxyBlocksRedirectsAndSubresources(t *testing.T) {
	var internalHits atomic.Int32
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		internalHits.Add(1)
		_, _ = w.Write([]byte("secreto"))
	}))
	defer internal.Close()
	forbidden := strings.Replace(internal.URL, "127.0.0.1", "127.0.0.2", 1)

	var allowedHits atomic.Int32
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, forbidden+"/metadatos", http.StatusFound)
		case "/ok.png":
			allowedHits.Add(1)
		default:
			_, _ = w.Write([]byte(`<html><body><h1>Página pública</h1>
				<img src="/ok.png"><img src="` + forbidden + `/pixel.png">
				<script>fetch("` + forbidden + `/api").catch(() => {})</script></body></html>`))
		}
	}))
	defer site.Close()
	policy := netguard.Policy{AllowPrivate: true, Blocked: []netip.Prefix{netip.MustParsePrefix("127.0.0.2/32")}}
	session := openWith(t, Options{Policy: policy}, application.BrowserOptions{})

	if err := session.Navigate(context.Background(), site.URL+"/page"); err != nil {
		t.Fatal(err)
	}
	if allowedHits.Load() != 1 {
		t.Errorf("el subrecurso permitido debía cargarse: %d", allowedHits.Load())
	}
	if err := session.Navigate(context.Background(), site.URL+"/redirect"); !errors.Is(err, netguard.ErrForbidden) {
		t.Errorf("redirección a destino prohibido = %v", err)
	}
	if err := session.Navigate(context.Background(), forbidden+"/directo"); !errors.Is(err, netguard.ErrForbidden) {
		t.Errorf("navegación directa a destino prohibido = %v", err)
	}
	if internalHits.Load() != 0 {
		t.Errorf("el destino prohibido recibió %d peticiones", internalHits.Load())
	}
}

func TestSaveDiagnosticsWritesScreenshotAndTrace(t *testing.T) {
	session := openWith(t, localOptions, application.BrowserOptions{Trace: true})
	if err := session.Navigate(context.Background(), demoServer(t)+"/login.html"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "job-1")

	files, err := session.SaveDiagnostics(dir)
	if err != nil || len(files) != 2 {
		t.Fatalf("archivos = %v, err = %v", files, err)
	}
	for _, name := range []string{application.DiagnosticScreenshot, application.DiagnosticTrace} {
		if info, err := os.Stat(filepath.Join(dir, name)); err != nil || info.Size() < 1000 {
			t.Errorf("%s: %v (%v)", name, err, info)
		}
	}
}

// Recorrido completo: una página de login deja el trabajo bloqueado y con diagnóstico.
func TestBlockedRunLeavesDiagnostics(t *testing.T) {
	launcher := NewLauncher(localOptions)
	defer launcher.Close()
	base := demoServer(t)
	recipe := domain.Recipe{
		Name: "x", StartURL: base + "/login.html", ItemLocator: "article.product",
		Fields: []domain.FieldDefinition{{Name: "nombre", Type: domain.FieldText, Locator: ".name", Required: true}},
	}
	dir := filepath.Join(t.TempDir(), "job-2")

	result, err := application.NewService(launcher, application.ServiceOptions{}).Run(context.Background(),
		domain.ScrapeRequest{Recipe: &recipe, Headless: true, DiagnosticsDir: dir}, nil)
	if !errors.Is(err, domain.ErrBlocked) {
		t.Fatalf("err = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, application.DiagnosticScreenshot)); statErr != nil ||
		!strings.Contains(strings.Join(result.Warnings, "|"), "diagnóstico guardado") {
		t.Errorf("captura: %v, avisos: %v", statErr, result.Warnings)
	}
}
