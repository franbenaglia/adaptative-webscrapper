package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"adaptive-scraper/internal/domain"
)

type fakePage struct {
	url     string
	records []domain.RawRecord
}

// fakeBrowser simula un sitio paginado y registra el uso de la sesión.
type fakeBrowser struct {
	pages      []fakePage
	current    int
	opened     int
	closed     int
	options    BrowserOptions
	extractErr error
	onExtract  func()
}

func (f *fakeBrowser) Open(_ context.Context, options BrowserOptions) (BrowserSession, error) {
	f.opened++
	f.options = options
	return f, nil
}
func (f *fakeBrowser) Navigate(context.Context, string) error { return nil }
func (f *fakeBrowser) URL() string                            { return f.pages[f.current].url }
func (f *fakeBrowser) Extract(context.Context, domain.Recipe) ([]domain.RawRecord, error) {
	if f.onExtract != nil {
		f.onExtract()
	}
	return f.pages[f.current].records, f.extractErr
}
func (f *fakeBrowser) GoToNextPage(context.Context, domain.PaginationRule) (bool, error) {
	if f.current+1 >= len(f.pages) {
		return false, nil
	}
	f.current++
	return true, nil
}
func (f *fakeBrowser) Close() error { f.closed++; return nil }

func recipe() *domain.Recipe {
	return &domain.Recipe{
		Name:        "demo",
		StartURL:    "https://shop.example/p1",
		ItemLocator: ".product",
		Fields: []domain.FieldDefinition{
			{Name: "name", Type: domain.FieldText, Required: true},
			{Name: "price", Type: domain.FieldNumber, Required: true},
		},
		Pagination: domain.PaginationRule{Type: domain.PaginationNextButton, NextLocator: ".next"},
	}
}

func threePages() *fakeBrowser {
	return &fakeBrowser{pages: []fakePage{
		{"https://shop.example/p1", []domain.RawRecord{{"name": "A", "price": "$ 1,50"}, {"name": "B", "price": "2"}}},
		{"https://shop.example/p2", []domain.RawRecord{{"name": "C", "price": "3"}, {"name": "D"}}},
		{"https://shop.example/p3", []domain.RawRecord{{"name": "E", "price": "5"}}},
	}}
}

func TestRunWalksAllPages(t *testing.T) {
	browser := threePages()
	var events []Event
	svc := NewService(browser)
	sink := ProgressFunc(func(e Event) { events = append(events, e) })

	result, err := svc.Run(context.Background(), domain.ScrapeRequest{Recipe: recipe(), Headless: true}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if result.PagesVisited != 3 || len(result.Records) != 5 {
		t.Fatalf("páginas=%d registros=%d", result.PagesVisited, len(result.Records))
	}
	if result.Records[0]["price"] != 1.5 || result.Records[3]["price"] != nil {
		t.Errorf("conversión incorrecta: %v", result.Records)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], `"price" ausente en 1 de 5`) {
		t.Errorf("avisos = %v", result.Warnings)
	}
	if browser.opened != 1 || browser.closed != 1 || !browser.options.Headless {
		t.Errorf("sesión mal gestionada: %+v", browser)
	}
	if len(events) != 6 || events[0].Type != domain.EventPageOpened || events[5].Records != 5 || len(events[3].NewRecords) != 2 {
		t.Errorf("eventos = %+v", events)
	}
}

func TestRunRespectsLimits(t *testing.T) {
	result, err := NewService(threePages()).Run(context.Background(), domain.ScrapeRequest{Recipe: recipe(), MaxPages: 2}, nil)
	if err != nil || result.PagesVisited != 2 || len(result.Records) != 4 {
		t.Fatalf("max-pages: páginas=%d registros=%d err=%v", result.PagesVisited, len(result.Records), err)
	}

	result, err = NewService(threePages()).Run(context.Background(), domain.ScrapeRequest{Recipe: recipe(), MaxRecords: 3}, nil)
	if err != nil || result.PagesVisited != 2 || len(result.Records) != 3 {
		t.Fatalf("max-records: páginas=%d registros=%d err=%v", result.PagesVisited, len(result.Records), err)
	}
}

func TestRunWithoutPaginationReadsOnePage(t *testing.T) {
	r := recipe()
	r.Pagination = domain.PaginationRule{}
	result, err := NewService(threePages()).Run(context.Background(), domain.ScrapeRequest{Recipe: r}, nil)
	if err != nil || result.PagesVisited != 1 {
		t.Fatalf("páginas=%d err=%v", result.PagesVisited, err)
	}
}

func TestRunStopsWhenPaginationRepeatsContent(t *testing.T) {
	same := []domain.RawRecord{{"name": "A", "price": "1"}}
	browser := &fakeBrowser{pages: []fakePage{{"https://shop.example/p1", same}, {"https://shop.example/p1", same}, {"https://shop.example/p1", same}}}

	result, err := NewService(browser).Run(context.Background(), domain.ScrapeRequest{Recipe: recipe()}, nil)
	if err != nil || result.PagesVisited != 1 || len(result.Records) != 1 || len(result.Warnings) != 1 {
		t.Fatalf("resultado = %+v err=%v", result, err)
	}
}

func TestRunStopsWhenLeavingAllowedHosts(t *testing.T) {
	browser := threePages()
	browser.pages[1].url = "https://evil.example/p2"

	result, err := NewService(browser).Run(context.Background(), domain.ScrapeRequest{Recipe: recipe()}, nil)
	if err == nil || !strings.Contains(err.Error(), "hosts permitidos") {
		t.Fatalf("se esperaba error de host, se obtuvo %v", err)
	}
	if len(result.Records) != 2 || browser.closed != 1 {
		t.Errorf("debe conservar lo extraído y cerrar la sesión: registros=%d cierres=%d", len(result.Records), browser.closed)
	}
}

func TestRunClosesSessionOnFailureAndKeepsPartialResult(t *testing.T) {
	browser := threePages()
	boom := errors.New("boom")
	calls := 0
	browser.onExtract = func() {
		if calls++; calls == 2 {
			browser.extractErr = boom
		}
	}

	result, err := NewService(browser).Run(context.Background(), domain.ScrapeRequest{Recipe: recipe()}, nil)
	if !errors.Is(err, boom) || len(result.Records) != 2 || browser.closed != 1 {
		t.Fatalf("err=%v registros=%d cierres=%d", err, len(result.Records), browser.closed)
	}
}

func TestRunStopsOnCancellation(t *testing.T) {
	browser := threePages()
	ctx, cancel := context.WithCancel(context.Background())
	browser.onExtract = cancel

	result, err := NewService(browser).Run(ctx, domain.ScrapeRequest{Recipe: recipe()}, nil)
	if !errors.Is(err, context.Canceled) || result.PagesVisited != 1 || browser.closed != 1 {
		t.Fatalf("err=%v páginas=%d cierres=%d", err, result.PagesVisited, browser.closed)
	}
}

func TestRunRejectsInvalidRequestWithoutOpeningBrowser(t *testing.T) {
	browser := threePages()
	_, err := NewService(browser).Run(context.Background(), domain.ScrapeRequest{}, nil)
	if !errors.Is(err, domain.ErrInvalidRequest) || browser.opened != 0 {
		t.Fatalf("err=%v aperturas=%d", err, browser.opened)
	}
}
