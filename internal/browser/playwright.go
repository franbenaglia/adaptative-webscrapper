// Package browser adapta Playwright a los puertos de application. Es el único
// paquete que importa playwright-go.
package browser

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mxschmitt/playwright-go"

	"adaptive-scraper/internal/application"
	"adaptive-scraper/internal/domain"
	"adaptive-scraper/internal/netguard"
)

// Tiempo que se espera a que un clic en "siguiente" cambie la URL antes de
// asumir que la página se actualizó sin navegar.
const nextPageURLTimeout = 3 * time.Second

// extractScript es fijo: la receta solo aporta datos (selectores y nombres),
// nunca código.
const extractScript = `(items, fields) => items.map((item) => {
  const out = {};
  for (const f of fields) {
    const el = f.locator ? item.querySelector(f.locator) : item;
    if (!el) continue;
    const v = f.attribute ? el.getAttribute(f.attribute) : (el.innerText ?? el.textContent);
    if (v !== null && v !== undefined) out[f.name] = String(v);
  }
  return out;
})`

// Options ajusta el adaptador.
type Options struct {
	// Policy decide a qué direcciones puede llegar el navegador.
	Policy netguard.Policy
	// MaxBytesPerConnection limita lo que el navegador recibe por conexión; 0 es sin límite.
	MaxBytesPerConnection int64
}

// Launcher implementa application.BrowserLauncher con Chromium. El driver de
// Playwright y el proxy de filtrado se inician en el primer Open y se
// comparten entre sesiones.
type Launcher struct {
	options Options
	mu      sync.Mutex
	pw      *playwright.Playwright
	proxy   *netguard.Proxy
}

var _ application.BrowserLauncher = (*Launcher)(nil)

func NewLauncher(options Options) *Launcher { return &Launcher{options: options} }

func (l *Launcher) start() (*playwright.Playwright, *netguard.Proxy, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.proxy == nil {
		proxy, err := netguard.StartProxy(l.options.Policy, l.options.MaxBytesPerConnection)
		if err != nil {
			return nil, nil, fmt.Errorf("iniciar el proxy de filtrado: %w", err)
		}
		l.proxy = proxy
	}
	if l.pw == nil {
		pw, err := playwright.Run()
		if err != nil {
			return nil, nil, fmt.Errorf("iniciar Playwright (¿falta `make install-browsers`?): %w", err)
		}
		l.pw = pw
	}
	return l.pw, l.proxy, nil
}

// Open lanza un navegador con un contexto aislado. Al cancelarse ctx el
// navegador se cierra, lo que interrumpe cualquier operación en curso.
func (l *Launcher) Open(ctx context.Context, options application.BrowserOptions) (application.BrowserSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pw, proxy, err := l.start()
	if err != nil {
		return nil, err
	}
	br, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(options.Headless),
		// Todo el tráfico pasa por el proxy, que aplica la política de red.
		// "<-loopback>" anula la excepción que Chromium hace con localhost.
		Proxy: &playwright.Proxy{Server: proxy.URL(), Bypass: playwright.String("<-loopback>")},
	})
	if err != nil {
		return nil, fmt.Errorf("lanzar Chromium: %w", err)
	}
	s := &session{browser: br, policy: l.options.Policy, done: make(chan struct{})}
	bctx, err := br.NewContext(playwright.BrowserNewContextOptions{AcceptDownloads: playwright.Bool(false)})
	if err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("crear contexto: %w", err)
	}
	bctx.SetDefaultTimeout(ms(options.ActionTimeout))
	bctx.SetDefaultNavigationTimeout(ms(options.NavigationTimeout))
	if options.Trace {
		if err := bctx.Tracing().Start(playwright.TracingStartOptions{Screenshots: playwright.Bool(true), Snapshots: playwright.Bool(true)}); err != nil {
			_ = s.Close()
			return nil, fmt.Errorf("iniciar la traza: %w", err)
		}
		s.tracing = bctx.Tracing()
	}
	if s.page, err = bctx.NewPage(); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("crear página: %w", err)
	}
	go func() {
		select {
		case <-ctx.Done():
			_ = s.Close()
		case <-s.done:
		}
	}()
	return s, nil
}

// Close detiene el driver de Playwright y el proxy. Las sesiones deben cerrarse antes.
func (l *Launcher) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	var err error
	if l.pw != nil {
		err = l.pw.Stop()
		l.pw = nil
	}
	if l.proxy != nil {
		err = errors.Join(err, l.proxy.Close())
		l.proxy = nil
	}
	return err
}

type session struct {
	browser   playwright.Browser
	page      playwright.Page
	policy    netguard.Policy
	tracing   playwright.Tracing // nil si no se pidió traza
	done      chan struct{}
	closeOnce sync.Once
	closeErr  error
}

func (s *session) Navigate(ctx context.Context, rawURL string) error {
	// Comprobación previa para dar un error claro; quien hace cumplir la
	// política en todas las peticiones es el proxy.
	if err := s.policy.CheckURL(ctx, rawURL); err != nil {
		return err
	}
	response, err := s.page.Goto(rawURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateLoad})
	if err != nil {
		if strings.Contains(err.Error(), "ERR_TUNNEL_CONNECTION_FAILED") {
			return fmt.Errorf("%w (o el destino no respondió): %s", netguard.ErrForbidden, rawURL)
		}
		return ctxErr(ctx, err)
	}
	if response == nil {
		return nil
	}
	if marker, _ := response.HeaderValue(netguard.BlockedHeader); marker != "" {
		return fmt.Errorf("%w: la página redirige a %s", netguard.ErrForbidden, response.URL())
	}
	switch status := response.Status(); {
	case status == 401 || status == 403 || status == 429:
		return fmt.Errorf("%w: el servidor respondió %d", domain.ErrBlocked, status)
	case status >= 400:
		return fmt.Errorf("el servidor respondió %d", status)
	}
	return nil
}

func (s *session) URL() string { return s.page.URL() }

func (s *session) Extract(ctx context.Context, recipe domain.Recipe) ([]domain.RawRecord, error) {
	fields := make([]any, len(recipe.Fields))
	for i, f := range recipe.Fields {
		fields[i] = map[string]any{"name": f.Name, "locator": f.Locator, "attribute": f.Attribute}
	}
	out, err := s.page.Locator(recipe.ItemLocator).EvaluateAll(extractScript, fields)
	if err != nil {
		return nil, ctxErr(ctx, err)
	}
	items, ok := out.([]any)
	if !ok {
		return nil, fmt.Errorf("resultado inesperado de la extracción: %T", out)
	}
	records := make([]domain.RawRecord, 0, len(items))
	for _, item := range items {
		raw := domain.RawRecord{}
		if m, ok := item.(map[string]any); ok {
			for name, v := range m {
				if str, ok := v.(string); ok {
					raw[name] = str
				}
			}
		}
		records = append(records, raw)
	}
	return records, nil
}

func (s *session) GoToNextPage(ctx context.Context, rule domain.PaginationRule) (bool, error) {
	if rule.Type != domain.PaginationNextButton {
		return false, nil
	}
	next := s.page.Locator(rule.NextLocator).First()
	if n, err := next.Count(); err != nil || n == 0 {
		return false, ctxErr(ctx, err)
	}
	if visible, err := next.IsVisible(); err != nil || !visible {
		return false, ctxErr(ctx, err)
	}
	if enabled, err := next.IsEnabled(); err != nil || !enabled {
		return false, ctxErr(ctx, err)
	}
	previous := s.page.URL()
	if err := next.Click(); err != nil {
		return false, ctxErr(ctx, err)
	}
	err := s.page.WaitForURL(func(u string) bool { return u != previous }, playwright.PageWaitForURLOptions{
		Timeout:   playwright.Float(ms(nextPageURLTimeout)),
		WaitUntil: playwright.WaitUntilStateLoad,
	})
	if errors.Is(err, playwright.ErrTimeout) {
		// Paginación sin navegación: esperar a que termine la carga de datos.
		err = s.page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{State: playwright.LoadStateNetworkidle})
	}
	if err != nil {
		return false, ctxErr(ctx, err)
	}
	return true, nil
}

// SaveDiagnostics guarda una captura de la página y, si se pidió al abrir la
// sesión, la traza de Playwright. Devuelve los archivos escritos.
func (s *session) SaveDiagnostics(dir string) ([]string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	var files []string
	var errs []error
	screenshot := filepath.Join(dir, application.DiagnosticScreenshot)
	if _, err := s.page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String(screenshot), FullPage: playwright.Bool(true)}); err != nil {
		errs = append(errs, fmt.Errorf("captura: %w", err))
	} else {
		files = append(files, screenshot)
	}
	if s.tracing != nil {
		trace := filepath.Join(dir, application.DiagnosticTrace)
		if err := s.tracing.Stop(trace); err != nil {
			errs = append(errs, fmt.Errorf("traza: %w", err))
		} else {
			files = append(files, trace)
		}
		s.tracing = nil
	}
	return files, errors.Join(errs...)
}

func (s *session) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
		s.closeErr = s.browser.Close()
	})
	return s.closeErr
}

// ctxErr da prioridad a la cancelación: al cancelar se cierra el navegador y
// Playwright devuelve un error genérico de "target closed".
func ctxErr(ctx context.Context, err error) error {
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func ms(d time.Duration) float64 { return float64(d.Milliseconds()) }
