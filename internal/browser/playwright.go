// Package browser adapta Playwright a los puertos de application. Es el único
// paquete que importa playwright-go.
package browser

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mxschmitt/playwright-go"

	"adaptive-scraper/internal/application"
	"adaptive-scraper/internal/domain"
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

// Launcher implementa application.BrowserLauncher con Chromium. El driver de
// Playwright se inicia en el primer Open y se comparte entre sesiones.
type Launcher struct {
	mu sync.Mutex
	pw *playwright.Playwright
}

var _ application.BrowserLauncher = (*Launcher)(nil)

func NewLauncher() *Launcher { return &Launcher{} }

func (l *Launcher) driver() (*playwright.Playwright, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.pw == nil {
		pw, err := playwright.Run()
		if err != nil {
			return nil, fmt.Errorf("iniciar Playwright (¿falta `make install-browsers`?): %w", err)
		}
		l.pw = pw
	}
	return l.pw, nil
}

// Open lanza un navegador con un contexto aislado. Al cancelarse ctx el
// navegador se cierra, lo que interrumpe cualquier operación en curso.
func (l *Launcher) Open(ctx context.Context, options application.BrowserOptions) (application.BrowserSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pw, err := l.driver()
	if err != nil {
		return nil, err
	}
	br, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(options.Headless)})
	if err != nil {
		return nil, fmt.Errorf("lanzar Chromium: %w", err)
	}
	s := &session{browser: br, done: make(chan struct{})}
	bctx, err := br.NewContext()
	if err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("crear contexto: %w", err)
	}
	bctx.SetDefaultTimeout(ms(options.ActionTimeout))
	bctx.SetDefaultNavigationTimeout(ms(options.NavigationTimeout))
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

// Close detiene el driver de Playwright. Las sesiones deben cerrarse antes.
func (l *Launcher) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.pw == nil {
		return nil
	}
	err := l.pw.Stop()
	l.pw = nil
	return err
}

type session struct {
	browser   playwright.Browser
	page      playwright.Page
	done      chan struct{}
	closeOnce sync.Once
	closeErr  error
}

func (s *session) Navigate(ctx context.Context, rawURL string) error {
	_, err := s.page.Goto(rawURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateLoad})
	return ctxErr(ctx, err)
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
