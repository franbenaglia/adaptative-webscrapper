package application

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"time"

	"adaptive-scraper/internal/domain"
)

// ServiceOptions ajusta el motor. Intelligence nil desactiva el
// descubrimiento y la reparación de recetas.
type ServiceOptions struct {
	Intelligence IntelligenceProvider
	// MaxRepairs es el número máximo de intentos de reparación por ejecución.
	MaxRepairs int
}

// Service implementa ScrapingService sobre un BrowserLauncher.
type Service struct {
	launcher          BrowserLauncher
	ai                IntelligenceProvider
	maxRepairs        int
	navigationTimeout time.Duration
	actionTimeout     time.Duration
}

var _ ScrapingService = (*Service)(nil)

// NewService crea el motor de extracción.
func NewService(launcher BrowserLauncher, options ServiceOptions) *Service {
	return &Service{
		launcher:          launcher,
		ai:                options.Intelligence,
		maxRepairs:        options.MaxRepairs,
		navigationTimeout: 30 * time.Second,
		actionTimeout:     10 * time.Second,
	}
}

// Run ejecuta una extracción hasta agotar páginas, registros o la paginación.
//
// Con receta, las ejecuciones son deterministas: el modelo solo interviene si
// la receta deja de funcionar en la primera página, para repararla. Sin
// receta, el modelo la descubre a partir de la instrucción.
//
// Si falla a mitad de camino devuelve lo extraído hasta ese momento junto con
// el error.
func (s *Service) Run(ctx context.Context, request domain.ScrapeRequest, sink ProgressSink) (result domain.ScrapeResult, err error) {
	if sink == nil {
		sink = ProgressFunc(func(Event) {})
	}
	request, err = request.Normalize()
	if err != nil {
		return result, err
	}
	if request.Recipe == nil && s.ai == nil {
		return result, fmt.Errorf("%w: la IA está desactivada; indique una receta", domain.ErrInvalidRequest)
	}

	session, err := s.launcher.Open(ctx, BrowserOptions{
		Headless:          request.Headless,
		NavigationTimeout: s.navigationTimeout,
		ActionTimeout:     s.actionTimeout,
	})
	if err != nil {
		return result, fmt.Errorf("abrir navegador: %w", err)
	}
	defer func() {
		if cerr := session.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("cerrar navegador: %w", cerr)
		}
	}()

	if err := session.Navigate(ctx, request.URL); err != nil {
		return result, fmt.Errorf("navegar a %s: %w", request.URL, err)
	}
	pageURL := session.URL()
	result.PagesVisited = 1
	sink.Emit(Event{Type: domain.EventPageOpened, Page: 1, URL: pageURL})

	var recipe domain.Recipe
	setRecipe := func(r domain.Recipe, origin string) {
		recipe = r
		result.Recipe, result.RecipeOrigin, result.Fields = &recipe, origin, r.Fields
	}
	if request.Recipe != nil {
		setRecipe(*request.Recipe, domain.RecipeProvided)
		if err := checkHost(recipe, pageURL); err != nil {
			return result, err
		}
	} else {
		discovered, err := s.discover(ctx, session, request, pageURL)
		if err != nil {
			return result, err
		}
		setRecipe(discovered, domain.RecipeDiscovered)
		sink.Emit(Event{Type: domain.EventRecipeDiscovered, Page: 1, URL: pageURL, Recipe: &discovered,
			RecipeOrigin: domain.RecipeDiscovered, Message: fmt.Sprintf("%d campos", len(discovered.Fields))})
	}

	raws, err := session.Extract(ctx, recipe)
	if err != nil {
		return result, fmt.Errorf("extraer página 1: %w", err)
	}
	failure := domain.AssessExtraction(recipe.Fields, raws, pageURL)
	if failure != nil && failure.Items == 0 {
		if err := blocked(ctx, session); err != nil {
			return result, err
		}
	}
	for attempt := 1; failure != nil && s.ai != nil && attempt <= s.maxRepairs; attempt++ {
		sink.Emit(Event{Type: domain.EventRecipeRepairing, Page: 1, URL: pageURL,
			Message: fmt.Sprintf("intento %d de %d: %s", attempt, s.maxRepairs, failure.Error())})
		repaired, repairedRaws, repairErr := s.repair(ctx, session, recipe, *failure, pageURL)
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if repairErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("intento de reparación %d fallido: %v", attempt, repairErr))
			continue
		}
		origin := domain.RecipeRepaired
		if result.RecipeOrigin == domain.RecipeDiscovered {
			origin = domain.RecipeDiscovered
		}
		setRecipe(repaired, origin)
		raws, failure = repairedRaws, nil
	}
	switch {
	case failure == nil && result.RecipeOrigin != domain.RecipeProvided:
		sink.Emit(Event{Type: domain.EventRecipeValidated, Page: 1, URL: pageURL, Recipe: &recipe,
			RecipeOrigin: result.RecipeOrigin, Message: fmt.Sprintf("%d elementos en la primera página", len(raws))})
	case failure != nil && result.RecipeOrigin == domain.RecipeDiscovered && failure.Items == 0:
		return result, fmt.Errorf("la receta descubierta no extrae nada: %s", failure.Error())
	case failure != nil && failure.Items > 0:
		result.Warnings = append(result.Warnings, "la receta no encaja bien con la página: "+failure.Error())
	}

	missing := map[string]int{}
	seen := map[string]bool{}
	duplicates := 0
	var previous []domain.RawRecord

	for {
		// Un "siguiente" que no cambia el contenido repetiría la página sin fin.
		if result.PagesVisited > 1 && len(raws) > 0 && reflect.DeepEqual(raws, previous) {
			result.PagesVisited--
			result.Warnings = append(result.Warnings, "la paginación no cambió el contenido; se detuvo el recorrido")
			break
		}
		previous = raws
		if len(raws) == 0 {
			result.Warnings = append(result.Warnings, fmt.Sprintf("página %d: ningún elemento coincide con %q", result.PagesVisited, recipe.ItemLocator))
		}

		added := len(result.Records)
		for _, raw := range raws {
			if len(result.Records) >= request.MaxRecords {
				break
			}
			record, miss := domain.BuildRecord(recipe.Fields, raw, pageURL)
			for _, name := range miss {
				missing[name]++
			}
			key := fmt.Sprint(record)
			if seen[key] {
				duplicates++
			}
			seen[key] = true
			result.Records = append(result.Records, record)
		}
		sink.Emit(Event{Type: domain.EventRecordsExtracted, Page: result.PagesVisited, URL: pageURL,
			Records: len(result.Records), NewRecords: result.Records[added:]})

		if len(result.Records) >= request.MaxRecords || result.PagesVisited >= request.MaxPages || !recipe.HasPagination() {
			break
		}
		more, err := session.GoToNextPage(ctx, recipe.Pagination)
		if err != nil {
			return result, fmt.Errorf("pasar a la página %d: %w", result.PagesVisited+1, err)
		}
		if !more {
			break
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		pageURL = session.URL()
		if err := checkHost(recipe, pageURL); err != nil {
			return result, err
		}
		result.PagesVisited++
		sink.Emit(Event{Type: domain.EventPageOpened, Page: result.PagesVisited, URL: pageURL, Records: len(result.Records)})
		if raws, err = session.Extract(ctx, recipe); err != nil {
			return result, fmt.Errorf("extraer página %d: %w", result.PagesVisited, err)
		}
	}

	for _, f := range recipe.Fields {
		if n := missing[f.Name]; n > 0 {
			result.Warnings = append(result.Warnings, fmt.Sprintf("campo requerido %q ausente en %d de %d registros", f.Name, n, len(result.Records)))
		}
	}
	if duplicates > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("%d registros duplicados", duplicates))
	}
	return result, nil
}

// discover pide al modelo una receta para la página actual. La respuesta se
// trata como entrada no confiable: se completa con datos propios y se valida.
func (s *Service) discover(ctx context.Context, session BrowserSession, request domain.ScrapeRequest, pageURL string) (domain.Recipe, error) {
	if err := blocked(ctx, session); err != nil {
		return domain.Recipe{}, err
	}
	snapshot, err := session.Snapshot(ctx)
	if err != nil {
		return domain.Recipe{}, fmt.Errorf("resumir la página: %w", err)
	}
	if len(snapshot.Groups) == 0 {
		return domain.Recipe{}, errors.New("no se encontraron elementos repetidos en la página; no hay nada que proponer al modelo")
	}
	recipe, err := s.ai.DiscoverRecipe(ctx, snapshot, request.Instruction)
	if err != nil {
		return domain.Recipe{}, fmt.Errorf("descubrir la receta: %w", err)
	}
	recipe.ID, recipe.Version = "", 0
	recipe.StartURL = pageURL
	recipe.AllowedHosts = hostsOf(request.URL, pageURL)
	if strings.TrimSpace(recipe.Name) == "" {
		recipe.Name = truncate(request.Instruction, 80)
	}
	if err := recipe.Validate(); err != nil {
		return domain.Recipe{}, fmt.Errorf("el modelo propuso una receta inválida: %w", err)
	}
	return recipe, nil
}

// repair pide al modelo una receta corregida y la prueba en la página actual.
// Solo la devuelve si extrae correctamente; conserva la identidad y los
// nombres de campo de la anterior.
func (s *Service) repair(ctx context.Context, session BrowserSession, previous domain.Recipe, failure domain.ExtractionFailure, pageURL string) (domain.Recipe, []domain.RawRecord, error) {
	snapshot, err := session.Snapshot(ctx)
	if err != nil {
		return previous, nil, fmt.Errorf("resumir la página: %w", err)
	}
	if len(snapshot.Groups) == 0 {
		return previous, nil, errors.New("no se encontraron elementos repetidos en la página")
	}
	repaired, err := s.ai.RepairRecipe(ctx, previous, snapshot, failure)
	if err != nil {
		return previous, nil, err
	}
	repaired.ID, repaired.Name, repaired.Version = previous.ID, previous.Name, previous.Version
	repaired.StartURL, repaired.AllowedHosts = previous.StartURL, previous.AllowedHosts
	if err := repaired.Validate(); err != nil {
		return previous, nil, fmt.Errorf("el modelo propuso una receta inválida: %w", err)
	}
	if !sameFields(previous.Fields, repaired.Fields) {
		return previous, nil, errors.New("la receta propuesta cambia los nombres o tipos de los campos")
	}
	raws, err := session.Extract(ctx, repaired)
	if err != nil {
		return previous, nil, err
	}
	if stillBroken := domain.AssessExtraction(repaired.Fields, raws, pageURL); stillBroken != nil {
		return previous, nil, fmt.Errorf("la receta propuesta tampoco funciona: %s", stillBroken.Error())
	}
	return repaired, raws, nil
}

// blocked devuelve domain.ErrBlocked si la página es un login, un CAPTCHA o
// un bloqueo. El motor nunca intenta sortearlos.
func blocked(ctx context.Context, session BrowserSession) error {
	reason, err := session.BlockReason(ctx)
	if err != nil {
		return fmt.Errorf("comprobar bloqueos: %w", err)
	}
	if reason != "" {
		return fmt.Errorf("%w: %s", domain.ErrBlocked, reason)
	}
	return nil
}

func sameFields(a, b []domain.FieldDefinition) bool {
	if len(a) != len(b) {
		return false
	}
	types := make(map[string]string, len(a))
	for _, f := range a {
		types[f.Name] = f.Type
	}
	for _, f := range b {
		if t, ok := types[f.Name]; !ok || t != f.Type {
			return false
		}
	}
	return true
}

func checkHost(recipe domain.Recipe, pageURL string) error {
	u, err := url.Parse(pageURL)
	if err != nil || !recipe.AllowsHost(u.Hostname()) || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("la navegación salió de los hosts permitidos (%s): %s",
			strings.Join(allowedHosts(recipe), ", "), pageURL)
	}
	return nil
}

func allowedHosts(recipe domain.Recipe) []string {
	if len(recipe.AllowedHosts) > 0 {
		return recipe.AllowedHosts
	}
	if u, err := url.Parse(recipe.StartURL); err == nil {
		return []string{u.Hostname()}
	}
	return nil
}

// hostsOf devuelve los hosts distintos de las URL dadas, en orden.
func hostsOf(rawURLs ...string) []string {
	var hosts []string
	for _, raw := range rawURLs {
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" {
			continue
		}
		host := strings.ToLower(u.Hostname())
		known := false
		for _, h := range hosts {
			known = known || h == host
		}
		if !known {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func truncate(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit-1]) + "…"
}
