package application_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"adaptive-scraper/internal/application"
	"adaptive-scraper/internal/domain"
	"adaptive-scraper/internal/storage/sqlite"
)

// engineFunc simula el motor de extracción.
type engineFunc func(ctx context.Context, request domain.ScrapeRequest, sink application.ProgressSink) (domain.ScrapeResult, error)

func (f engineFunc) Run(ctx context.Context, request domain.ScrapeRequest, sink application.ProgressSink) (domain.ScrapeResult, error) {
	return f(ctx, request, sink)
}

// emitPage reproduce lo que emite el motor real por cada página.
func emitPage(sink application.ProgressSink, page, total int, names ...string) {
	records := make([]domain.Record, len(names))
	for i, name := range names {
		records[i] = domain.Record{"name": name}
	}
	url := fmt.Sprintf("https://shop.example/p%d", page)
	sink.Emit(application.Event{Type: domain.EventPageOpened, Page: page, URL: url, Records: total - len(names)})
	sink.Emit(application.Event{Type: domain.EventRecordsExtracted, Page: page, URL: url, Records: total, NewRecords: records})
}

func twoPages(_ context.Context, _ domain.ScrapeRequest, sink application.ProgressSink) (domain.ScrapeResult, error) {
	emitPage(sink, 1, 2, "A", "B")
	emitPage(sink, 2, 3, "C")
	return domain.ScrapeResult{PagesVisited: 2, Warnings: []string{"aviso"}}, nil
}

func testRecipe() *domain.Recipe {
	return &domain.Recipe{
		Name: "demo", StartURL: "https://shop.example/p1", ItemLocator: ".p",
		Fields:     []domain.FieldDefinition{{Name: "name", Type: domain.FieldText}},
		Pagination: domain.PaginationRule{Type: domain.PaginationNextButton, NextLocator: ".next"},
	}
}

type fixture struct {
	jobs    *application.JobService
	recipes *application.RecipeService
	store   *sqlite.Store
}

func setup(t *testing.T, engine engineFunc, options application.JobOptions) fixture {
	t.Helper()
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return fixture{
		jobs:    application.NewJobService(engine, store.Jobs(), store.Recipes(), options),
		recipes: application.NewRecipeService(store.Recipes()),
		store:   store,
	}
}

func (f fixture) create(t *testing.T) domain.Job {
	t.Helper()
	job, err := f.jobs.Create(context.Background(), application.JobRequest{Recipe: testRecipe(), Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func eventTypes(events []domain.JobEvent) string {
	types := make([]string, len(events))
	for i, e := range events {
		types[i] = e.Type
	}
	return strings.Join(types, " ")
}

func TestCreateValidatesAndPersistsQueuedJob(t *testing.T) {
	ctx := context.Background()
	f := setup(t, twoPages, application.JobOptions{})

	job := f.create(t)
	stored, err := f.jobs.Get(ctx, job.ID)
	if err != nil || stored.Status != domain.JobQueued || stored.URL != "https://shop.example/p1" ||
		stored.MaxPages != domain.DefaultMaxPages || stored.MaxRecords != domain.DefaultMaxRecords || !stored.Headless {
		t.Fatalf("trabajo guardado = %+v, %v", stored, err)
	}

	saved, err := f.recipes.Create(ctx, *testRecipe())
	if err != nil {
		t.Fatal(err)
	}
	fromSaved, err := f.jobs.Create(ctx, application.JobRequest{RecipeID: saved.ID, MaxPages: 2})
	if err != nil || fromSaved.RecipeID != saved.ID || fromSaved.Recipe.Name != "demo" || fromSaved.MaxPages != 2 {
		t.Fatalf("trabajo desde receta guardada = %+v, %v", fromSaved, err)
	}

	bad := testRecipe()
	bad.Fields = nil
	cases := map[string]struct {
		request application.JobRequest
		want    error
	}{
		"sin receta":         {application.JobRequest{}, domain.ErrInvalidRequest},
		"receta e id":        {application.JobRequest{RecipeID: saved.ID, Recipe: testRecipe()}, domain.ErrInvalidRequest},
		"id inexistente":     {application.JobRequest{RecipeID: "nope"}, domain.ErrNotFound},
		"receta inválida":    {application.JobRequest{Recipe: bad}, domain.ErrInvalidRecipe},
		"host no permitido":  {application.JobRequest{Recipe: testRecipe(), URL: "https://evil.example/"}, domain.ErrInvalidRequest},
		"límite fuera rango": {application.JobRequest{Recipe: testRecipe(), MaxPages: -1}, domain.ErrInvalidRequest},
	}
	for name, c := range cases {
		if _, err := f.jobs.Create(ctx, c.request); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v; se esperaba %v", name, err, c.want)
		}
	}
	if all, _ := f.jobs.List(ctx, application.JobFilter{}); len(all) != 2 {
		t.Errorf("las solicitudes inválidas no deben crear trabajos: %d", len(all))
	}
}

func TestExecutePersistsRecordsAndEventsIncrementally(t *testing.T) {
	ctx := context.Background()
	var f fixture
	var jobID string
	engine := func(ctx context.Context, request domain.ScrapeRequest, sink application.ProgressSink) (domain.ScrapeResult, error) {
		emitPage(sink, 1, 2, "A", "B")
		// A mitad de ejecución ya debe verse el progreso guardado.
		mid, _ := f.jobs.Get(ctx, jobID)
		records, _ := f.jobs.Records(ctx, jobID, 0, 0)
		if mid.Status != domain.JobRunning || mid.RecordCount != 2 || mid.PagesVisited != 1 || len(records) != 2 {
			t.Errorf("progreso intermedio = %+v, %d registros", mid, len(records))
		}
		emitPage(sink, 2, 3, "C")
		return domain.ScrapeResult{PagesVisited: 2, Warnings: []string{"aviso"}}, nil
	}
	f = setup(t, engine, application.JobOptions{})
	jobID = f.create(t).ID

	var seen []domain.JobEvent
	job, err := f.jobs.Execute(ctx, jobID, func(e domain.JobEvent) { seen = append(seen, e) })
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != domain.JobCompleted || job.RecordCount != 3 || job.PagesVisited != 2 ||
		len(job.Warnings) != 1 || job.StartedAt == nil || job.FinishedAt == nil || job.Error != "" {
		t.Fatalf("trabajo final = %+v", job)
	}
	records, _ := f.jobs.Records(ctx, jobID, 0, 0)
	if len(records) != 3 || records[0]["name"] != "A" || records[2]["name"] != "C" {
		t.Errorf("registros = %v", records)
	}
	stored, _ := f.jobs.Events(ctx, jobID, 0)
	want := "job.started page.opened records.extracted page.opened records.extracted job.completed"
	if eventTypes(stored) != want || eventTypes(seen) != want {
		t.Errorf("eventos guardados = %q, vistos = %q", eventTypes(stored), eventTypes(seen))
	}
	if last := stored[len(stored)-1]; last.Seq != 6 || last.Records != 3 {
		t.Errorf("último evento = %+v", last)
	}
	if _, err := f.jobs.Execute(ctx, jobID, nil); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("reejecutar un trabajo terminado = %v", err)
	}
}

func TestExecuteFinalStatusDependsOnError(t *testing.T) {
	boom := errors.New("boom")
	cases := map[string]struct {
		engine  engineFunc
		status  domain.JobStatus
		wantErr error
		records int
	}{
		"fallo tardío": {func(_ context.Context, _ domain.ScrapeRequest, sink application.ProgressSink) (domain.ScrapeResult, error) {
			emitPage(sink, 1, 2, "A", "B")
			return domain.ScrapeResult{PagesVisited: 1}, boom
		}, domain.JobFailed, boom, 2},
		"bloqueo": {func(context.Context, domain.ScrapeRequest, application.ProgressSink) (domain.ScrapeResult, error) {
			return domain.ScrapeResult{}, fmt.Errorf("captcha detectado: %w", domain.ErrBlocked)
		}, domain.JobBlocked, domain.ErrBlocked, 0},
		"pánico": {func(context.Context, domain.ScrapeRequest, application.ProgressSink) (domain.ScrapeResult, error) {
			panic("algo se rompió")
		}, domain.JobFailed, nil, 0},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := setup(t, c.engine, application.JobOptions{})
			job, err := f.jobs.Execute(context.Background(), f.create(t).ID, nil)
			if err == nil || (c.wantErr != nil && !errors.Is(err, c.wantErr)) {
				t.Fatalf("err = %v", err)
			}
			stored, _ := f.jobs.Get(context.Background(), job.ID)
			if stored.Status != c.status || stored.Error == "" || stored.RecordCount != c.records || stored.FinishedAt == nil {
				t.Errorf("trabajo guardado = %+v", stored)
			}
			events, _ := f.jobs.Events(context.Background(), job.ID, 0)
			if last := events[len(events)-1]; last.Type != domain.TerminalEventType(c.status) || last.Message == "" {
				t.Errorf("último evento = %+v", last)
			}
		})
	}
}

func blockUntilCancelled(started chan<- struct{}) engineFunc {
	return func(ctx context.Context, _ domain.ScrapeRequest, sink application.ProgressSink) (domain.ScrapeResult, error) {
		emitPage(sink, 1, 1, "A")
		close(started)
		<-ctx.Done()
		return domain.ScrapeResult{PagesVisited: 1}, fmt.Errorf("extraer página 2: %w", ctx.Err())
	}
}

func TestCancelRunningJob(t *testing.T) {
	ctx := context.Background()
	started := make(chan struct{})
	f := setup(t, blockUntilCancelled(started), application.JobOptions{})
	id := f.create(t).ID

	done := make(chan error, 1)
	go func() {
		_, err := f.jobs.Execute(ctx, id, nil)
		done <- err
	}()
	<-started
	if job, err := f.jobs.Cancel(ctx, id); err != nil || job.Status != domain.JobRunning {
		t.Fatalf("Cancel = %+v, %v", job, err)
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute = %v", err)
	}
	job, _ := f.jobs.Get(ctx, id)
	if job.Status != domain.JobCancelled || job.RecordCount != 1 {
		t.Errorf("trabajo = %+v", job)
	}
	if _, err := f.jobs.Cancel(ctx, id); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("cancelar un trabajo terminado = %v", err)
	}
}

func TestCancelQueuedJobPreventsExecution(t *testing.T) {
	ctx := context.Background()
	calls := 0
	f := setup(t, func(context.Context, domain.ScrapeRequest, application.ProgressSink) (domain.ScrapeResult, error) {
		calls++
		return domain.ScrapeResult{}, nil
	}, application.JobOptions{})
	id := f.create(t).ID

	job, err := f.jobs.Cancel(ctx, id)
	if err != nil || job.Status != domain.JobCancelled || job.FinishedAt == nil {
		t.Fatalf("Cancel = %+v, %v", job, err)
	}
	if _, err := f.jobs.Execute(ctx, id, nil); !errors.Is(err, domain.ErrInvalidTransition) || calls != 0 {
		t.Errorf("Execute = %v, llamadas al motor = %d", err, calls)
	}
	if _, err := f.jobs.Cancel(ctx, "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("cancelar inexistente = %v", err)
	}
}

func TestExecuteParentCancellationStillSavesFinalState(t *testing.T) {
	started := make(chan struct{})
	f := setup(t, blockUntilCancelled(started), application.JobOptions{})
	id := f.create(t).ID
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()

	if _, err := f.jobs.Execute(ctx, id, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute = %v", err)
	}
	if job, _ := f.jobs.Get(context.Background(), id); job.Status != domain.JobCancelled {
		t.Errorf("estado = %s", job.Status)
	}
}

func TestExecuteTimeoutFailsJob(t *testing.T) {
	started := make(chan struct{})
	f := setup(t, blockUntilCancelled(started), application.JobOptions{Timeout: 20 * time.Millisecond})

	job, err := f.jobs.Execute(context.Background(), f.create(t).ID, nil)
	if err == nil || job.Status != domain.JobFailed || !strings.Contains(job.Error, "tiempo máximo") {
		t.Fatalf("trabajo = %+v, err = %v", job, err)
	}
}

func TestRecoverInterrupted(t *testing.T) {
	ctx := context.Background()
	f := setup(t, twoPages, application.JobOptions{})
	base := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	for i, status := range []domain.JobStatus{domain.JobQueued, domain.JobRunning, domain.JobQueued, domain.JobCompleted} {
		job := domain.Job{ID: fmt.Sprintf("j%d", i), Recipe: *testRecipe(), URL: "https://shop.example/p1", Status: status,
			MaxPages: 1, MaxRecords: 1, CreatedAt: base.Add(time.Duration(i) * time.Minute)}
		if err := f.store.Jobs().Create(ctx, job); err != nil {
			t.Fatal(err)
		}
	}

	queued, err := f.jobs.RecoverInterrupted(ctx)
	if err != nil || strings.Join(queued, ",") != "j0,j2" {
		t.Fatalf("en cola = %v, %v", queued, err)
	}
	interrupted, _ := f.jobs.Get(ctx, "j1")
	if interrupted.Status != domain.JobFailed || interrupted.Error == "" || interrupted.FinishedAt == nil {
		t.Errorf("trabajo interrumpido = %+v", interrupted)
	}
	if done, _ := f.jobs.Get(ctx, "j3"); done.Status != domain.JobCompleted {
		t.Errorf("un trabajo terminado no debe tocarse: %s", done.Status)
	}
}

func TestSubscribeReceivesLiveEvents(t *testing.T) {
	f := setup(t, twoPages, application.JobOptions{})
	id := f.create(t).ID
	events, unsubscribe := f.jobs.Subscribe(id)
	defer unsubscribe()

	if _, err := f.jobs.Execute(context.Background(), id, nil); err != nil {
		t.Fatal(err)
	}
	var got []domain.JobEvent
	for e := range events {
		got = append(got, e)
		if e.Terminal() {
			break
		}
	}
	if len(got) != 6 || got[0].Seq != 1 || got[5].Type != domain.EventJobCompleted {
		t.Errorf("eventos en vivo = %q", eventTypes(got))
	}
}

func TestEventHubDropsSlowSubscriber(t *testing.T) {
	hub := application.NewEventHub()
	slow, unsubscribe := hub.Subscribe("j1")
	for i := range 200 {
		hub.Publish(domain.JobEvent{JobID: "j1", Seq: int64(i + 1)})
	}
	count := 0
	for range slow {
		count++
	}
	if count == 0 || count >= 200 {
		t.Errorf("el suscriptor lento debió recibir solo su búfer y cerrarse: %d", count)
	}
	unsubscribe() // darse de baja dos veces o tras el cierre no debe fallar
	unsubscribe()
	hub.Publish(domain.JobEvent{JobID: "j1"})
}

func TestQueueLimitsConcurrencyAndRunsEverything(t *testing.T) {
	const workers, total = 2, 6
	var running, peak atomic.Int32
	var done sync.WaitGroup
	done.Add(total)
	f := setup(t, func(context.Context, domain.ScrapeRequest, application.ProgressSink) (domain.ScrapeResult, error) {
		defer done.Done()
		now := running.Add(1)
		for {
			old := peak.Load()
			if now <= old || peak.CompareAndSwap(old, now) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		running.Add(-1)
		return domain.ScrapeResult{PagesVisited: 1}, nil
	}, application.JobOptions{})

	ctx, cancel := context.WithCancel(context.Background())
	queue := application.NewQueue(f.jobs, workers, total)
	for range total {
		if err := queue.Submit(f.create(t).ID); err != nil {
			t.Fatal(err)
		}
	}
	queue.Start(ctx) // con todo ya encolado, los workers siempre tienen trabajo
	done.Wait()
	cancel()
	queue.Wait()

	if peak.Load() != workers {
		t.Errorf("concurrencia máxima = %d; se esperaba %d", peak.Load(), workers)
	}
	completed, _ := f.jobs.List(context.Background(), application.JobFilter{Status: domain.JobCompleted})
	if len(completed) != total {
		t.Errorf("completados = %d de %d", len(completed), total)
	}
}

func TestQueueRejectsWhenFull(t *testing.T) {
	f := setup(t, twoPages, application.JobOptions{})
	queue := application.NewQueue(f.jobs, 1, 1) // sin Start: nada consume
	if err := queue.Submit("a"); err != nil {
		t.Fatal(err)
	}
	if err := queue.Submit("b"); !errors.Is(err, application.ErrQueueFull) {
		t.Errorf("err = %v", err)
	}
}

func TestRecipeServiceVersions(t *testing.T) {
	ctx := context.Background()
	f := setup(t, twoPages, application.JobOptions{})

	created, err := f.recipes.Create(ctx, *testRecipe())
	if err != nil || created.ID == "" || created.Version != 1 {
		t.Fatalf("Create = %+v, %v", created, err)
	}
	changed := *testRecipe()
	changed.ItemLocator = ".card"
	updated, err := f.recipes.Update(ctx, created.ID, changed)
	if err != nil || updated.Version != 2 || updated.ID != created.ID {
		t.Fatalf("Update = %+v, %v", updated, err)
	}
	if versions, _ := f.store.Recipes().Versions(ctx, created.ID); len(versions) != 2 {
		t.Errorf("versiones guardadas = %d", len(versions))
	}

	changed.Fields = nil
	if _, err := f.recipes.Update(ctx, created.ID, changed); !errors.Is(err, domain.ErrInvalidRecipe) {
		t.Errorf("actualizar con receta inválida = %v", err)
	}
	if _, err := f.recipes.Update(ctx, "nope", *testRecipe()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("actualizar inexistente = %v", err)
	}
	if _, err := f.recipes.Create(ctx, changed); !errors.Is(err, domain.ErrInvalidRecipe) {
		t.Errorf("crear receta inválida = %v", err)
	}
}

// --- Trabajos con IA ---

func TestCreateFromInstructionRequiresAI(t *testing.T) {
	ctx := context.Background()
	request := application.JobRequest{URL: "https://shop.example/p1", Instruction: "nombre y precio"}

	off := setup(t, twoPages, application.JobOptions{})
	if _, err := off.jobs.Create(ctx, request); !errors.Is(err, domain.ErrInvalidRequest) || !strings.Contains(err.Error(), "AI_ENABLED") {
		t.Errorf("con la IA desactivada: %v", err)
	}

	on := setup(t, twoPages, application.JobOptions{AIEnabled: true})
	job, err := on.jobs.Create(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := on.jobs.Get(ctx, job.ID)
	if stored.Instruction != "nombre y precio" || !stored.Recipe.IsZero() || stored.Recipe.Fields == nil || stored.RecipeOrigin != "" {
		t.Errorf("trabajo guardado = %+v", stored)
	}
	if req := stored.Request(); req.Recipe != nil || req.Instruction != "nombre y precio" {
		t.Errorf("solicitud reconstruida = %+v", req)
	}
	for name, bad := range map[string]application.JobRequest{
		"sin URL":         {Instruction: "nombres"},
		"sin instrucción": {URL: "https://shop.example/p1"},
	} {
		if _, err := on.jobs.Create(ctx, bad); !errors.Is(err, domain.ErrInvalidRequest) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestExecuteStoresDiscoveredRecipe(t *testing.T) {
	ctx := context.Background()
	discovered := *testRecipe()
	discovered.Name = "nombre y precio"
	engine := func(_ context.Context, request domain.ScrapeRequest, sink application.ProgressSink) (domain.ScrapeResult, error) {
		if request.Recipe != nil || request.Instruction != "nombre y precio" {
			t.Errorf("el motor recibió %+v", request)
		}
		sink.Emit(application.Event{Type: domain.EventPageOpened, Page: 1, URL: request.URL})
		sink.Emit(application.Event{Type: domain.EventRecipeDiscovered, Page: 1, Recipe: &discovered, RecipeOrigin: domain.RecipeDiscovered, Message: "1 campos"})
		sink.Emit(application.Event{Type: domain.EventRecipeValidated, Page: 1, Recipe: &discovered, RecipeOrigin: domain.RecipeDiscovered})
		emitPage(sink, 1, 1, "A")
		return domain.ScrapeResult{PagesVisited: 1, Recipe: &discovered, RecipeOrigin: domain.RecipeDiscovered}, nil
	}
	f := setup(t, engine, application.JobOptions{AIEnabled: true})
	created, err := f.jobs.Create(ctx, application.JobRequest{URL: "https://shop.example/p1", Instruction: "nombre y precio"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.jobs.Execute(ctx, created.ID, nil); err != nil {
		t.Fatal(err)
	}
	job, _ := f.jobs.Get(ctx, created.ID)
	if job.Status != domain.JobCompleted || job.Recipe.ItemLocator != ".p" || job.RecipeOrigin != domain.RecipeDiscovered || job.RecordCount != 1 {
		t.Fatalf("trabajo = %+v", job)
	}
	events, _ := f.jobs.Events(ctx, job.ID, 0)
	if !strings.Contains(eventTypes(events), "recipe.discovered recipe.validated") || events[2].Message != "1 campos" {
		t.Errorf("eventos = %q / %+v", eventTypes(events), events[2])
	}
	// Una receta descubierta no se guarda sola: lo decide el usuario.
	if saved, _ := f.recipes.List(ctx); len(saved) != 0 {
		t.Errorf("recetas guardadas = %d", len(saved))
	}
}

func TestExecuteSavesRepairedRecipeAsNewVersion(t *testing.T) {
	ctx := context.Background()
	var engineErr error
	engine := func(_ context.Context, request domain.ScrapeRequest, sink application.ProgressSink) (domain.ScrapeResult, error) {
		repaired := *request.Recipe
		repaired.ItemLocator = ".card"
		emitPage(sink, 1, 1, "A")
		return domain.ScrapeResult{PagesVisited: 1, Recipe: &repaired, RecipeOrigin: domain.RecipeRepaired}, engineErr
	}
	f := setup(t, engine, application.JobOptions{AIEnabled: true})
	saved, _ := f.recipes.Create(ctx, *testRecipe())

	created, _ := f.jobs.Create(ctx, application.JobRequest{RecipeID: saved.ID})
	job, err := f.jobs.Execute(ctx, created.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	current, _ := f.recipes.Get(ctx, saved.ID)
	if current.Version != 2 || current.ItemLocator != ".card" || job.Recipe.Version != 2 || job.RecipeOrigin != domain.RecipeRepaired {
		t.Fatalf("receta guardada = %+v, trabajo = %+v", current, job.Recipe)
	}
	if len(job.Warnings) != 1 || !strings.Contains(job.Warnings[0], "versión 2") {
		t.Errorf("avisos = %v", job.Warnings)
	}
	if versions, _ := f.store.Recipes().Versions(ctx, saved.ID); len(versions) != 2 || versions[0].ItemLocator != ".p" {
		t.Errorf("historial de versiones = %+v", versions)
	}

	// Si el trabajo no termina bien, la receta guardada no se toca.
	engineErr = errors.New("boom")
	again, _ := f.jobs.Create(ctx, application.JobRequest{RecipeID: saved.ID})
	if _, err := f.jobs.Execute(ctx, again.ID, nil); err == nil {
		t.Fatal("se esperaba el error del motor")
	}
	if after, _ := f.recipes.Get(ctx, saved.ID); after.Version != 2 {
		t.Errorf("versión tras un trabajo fallido = %d", after.Version)
	}
}
