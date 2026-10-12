package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"adaptive-scraper/internal/application"
	"adaptive-scraper/internal/domain"
)

var (
	_ application.RecipeRepository = (*RecipeRepository)(nil)
	_ application.JobRepository    = (*JobRepository)(nil)
)

var t0 = time.Date(2026, 10, 12, 10, 0, 0, 123, time.UTC)

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sub", "test.db")
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, path
}

func sampleRecipe(id string) domain.Recipe {
	return domain.Recipe{
		ID: id, Name: "Demo " + id, StartURL: "https://shop.example/", ItemLocator: ".p", Version: 1,
		Fields:     []domain.FieldDefinition{{Name: "name", Type: domain.FieldText, Locator: ".n", Required: true}},
		Pagination: domain.PaginationRule{Type: domain.PaginationNextButton, NextLocator: ".next"},
	}
}

func sampleJob(id string, created time.Time) domain.Job {
	return domain.Job{
		ID: id, RecipeID: "r1", Recipe: sampleRecipe("r1"), URL: "https://shop.example/", Status: domain.JobQueued,
		MaxPages: 5, MaxRecords: 100, Headless: true, Warnings: []string{}, CreatedAt: created,
	}
}

func TestOpenIsIdempotentAndKeepsData(t *testing.T) {
	ctx := context.Background()
	store, path := openTemp(t)
	if err := store.Recipes().Create(ctx, sampleRecipe("r1"), t0); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	again, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reabrir no debe volver a migrar: %v", err)
	}
	defer again.Close()
	if _, err := again.Recipes().Get(ctx, "r1"); err != nil {
		t.Fatal(err)
	}
}

func TestRecipeRepository(t *testing.T) {
	ctx := context.Background()
	store, _ := openTemp(t)
	repo := store.Recipes()

	recipe := sampleRecipe("r1")
	if err := repo.Create(ctx, recipe, t0); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, recipe, t0); err == nil {
		t.Error("un id duplicado debe fallar")
	}
	got, err := repo.Get(ctx, "r1")
	if err != nil || !reflect.DeepEqual(got, recipe) {
		t.Fatalf("Get = %+v, %v", got, err)
	}

	recipe.Version, recipe.ItemLocator = 2, ".card"
	if err := repo.Update(ctx, recipe, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	versions, err := repo.Versions(ctx, "r1")
	if err != nil || len(versions) != 2 || versions[0].ItemLocator != ".p" || versions[1].ItemLocator != ".card" {
		t.Fatalf("versiones = %+v, %v", versions, err)
	}

	_ = repo.Create(ctx, sampleRecipe("a0"), t0)
	list, err := repo.List(ctx)
	if err != nil || len(list) != 2 || list[0].ID != "a0" {
		t.Fatalf("List = %+v, %v", list, err)
	}

	if err := repo.Delete(ctx, "r1"); err != nil {
		t.Fatal(err)
	}
	if versions, _ := repo.Versions(ctx, "r1"); len(versions) != 0 {
		t.Error("borrar la receta debe borrar sus versiones")
	}
	for name, err := range map[string]error{
		"Get":    func() error { _, err := repo.Get(ctx, "r1"); return err }(),
		"Update": repo.Update(ctx, recipe, t0),
		"Delete": repo.Delete(ctx, "r1"),
	} {
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s de una receta inexistente = %v", name, err)
		}
	}
}

func TestJobRepository(t *testing.T) {
	ctx := context.Background()
	store, _ := openTemp(t)
	repo := store.Jobs()

	job := sampleJob("j1", t0)
	if err := repo.Create(ctx, job); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, "j1")
	if err != nil || !reflect.DeepEqual(got, job) {
		t.Fatalf("Get = %+v, %v\nse esperaba %+v", got, err, job)
	}

	_ = job.Transition(domain.JobRunning, t0.Add(time.Second))
	_ = job.Transition(domain.JobFailed, t0.Add(time.Minute))
	job.PagesVisited, job.RecordCount, job.Warnings, job.Error = 2, 3, []string{"aviso"}, "boom"
	if err := repo.Update(ctx, job); err != nil {
		t.Fatal(err)
	}
	got, err = repo.Get(ctx, "j1")
	if err != nil || !reflect.DeepEqual(got, job) {
		t.Fatalf("tras Update = %+v, %v", got, err)
	}

	_ = repo.Create(ctx, sampleJob("j2", t0.Add(time.Hour)))
	_ = repo.Create(ctx, sampleJob("j3", t0.Add(2*time.Hour)))
	all, _ := repo.List(ctx, application.JobFilter{})
	if len(all) != 3 || all[0].ID != "j3" || all[2].ID != "j1" {
		t.Errorf("List debe ordenar del más reciente al más antiguo: %v", ids(all))
	}
	if limited, _ := repo.List(ctx, application.JobFilter{Limit: 1}); len(limited) != 1 || limited[0].ID != "j3" {
		t.Errorf("límite: %v", ids(limited))
	}
	if queued, _ := repo.List(ctx, application.JobFilter{Status: domain.JobQueued}); len(queued) != 2 {
		t.Errorf("filtro por estado: %v", ids(queued))
	}

	if _, err := repo.Get(ctx, "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Get inexistente = %v", err)
	}
	if err := repo.Update(ctx, sampleJob("nope", t0)); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Update inexistente = %v", err)
	}
}

func TestJobRecordsAreAppendedIncrementally(t *testing.T) {
	ctx := context.Background()
	store, _ := openTemp(t)
	repo := store.Jobs()
	_ = repo.Create(ctx, sampleJob("j1", t0))

	if err := repo.AppendRecords(ctx, "j1", 0, []domain.Record{{"name": "A", "price": 1.5}, {"name": "B", "price": nil}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.AppendRecords(ctx, "j1", 2, []domain.Record{{"name": "C", "price": 3.0}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.AppendRecords(ctx, "j1", 2, []domain.Record{{"name": "X"}}); err == nil {
		t.Error("reutilizar una posición debe fallar")
	}
	if err := repo.AppendRecords(ctx, "nope", 0, []domain.Record{{"name": "X"}}); err == nil {
		t.Error("un trabajo inexistente debe fallar por clave foránea")
	}

	all, err := repo.Records(ctx, "j1", 0, 0)
	want := []domain.Record{{"name": "A", "price": 1.5}, {"name": "B", "price": nil}, {"name": "C", "price": 3.0}}
	if err != nil || !reflect.DeepEqual(all, want) {
		t.Fatalf("Records = %v, %v", all, err)
	}
	if page, _ := repo.Records(ctx, "j1", 1, 1); len(page) != 1 || page[0]["name"] != "B" {
		t.Errorf("paginación = %v", page)
	}
}

func TestJobEventsGetIncreasingSeq(t *testing.T) {
	ctx := context.Background()
	store, _ := openTemp(t)
	repo := store.Jobs()
	_ = repo.Create(ctx, sampleJob("j1", t0))
	_ = repo.Create(ctx, sampleJob("j2", t0))

	first, err := repo.AppendEvent(ctx, domain.JobEvent{JobID: "j1", Type: domain.EventJobStarted, At: t0, URL: "https://shop.example/"})
	if err != nil || first.Seq != 1 {
		t.Fatalf("primer evento = %+v, %v", first, err)
	}
	second, _ := repo.AppendEvent(ctx, domain.JobEvent{JobID: "j1", Type: domain.EventRecordsExtracted, At: t0, Page: 1, Records: 3})
	other, _ := repo.AppendEvent(ctx, domain.JobEvent{JobID: "j2", Type: domain.EventJobFailed, At: t0, Message: "boom"})
	if second.Seq != 2 || other.Seq != 1 {
		t.Fatalf("seq = %d, %d", second.Seq, other.Seq)
	}

	events, err := repo.Events(ctx, "j1", 0)
	if err != nil || !reflect.DeepEqual(events, []domain.JobEvent{first, second}) {
		t.Fatalf("Events = %+v, %v", events, err)
	}
	if after, _ := repo.Events(ctx, "j1", 1); len(after) != 1 || after[0].Seq != 2 {
		t.Errorf("Events(after=1) = %+v", after)
	}
}

func ids(jobs []domain.Job) []string {
	out := make([]string, len(jobs))
	for i, j := range jobs {
		out[i] = j.ID
	}
	return out
}
