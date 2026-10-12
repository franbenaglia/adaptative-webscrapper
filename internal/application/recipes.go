package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"adaptive-scraper/internal/domain"
)

// NewID genera un identificador aleatorio de 16 caracteres hexadecimales.
func NewID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand no falla en los sistemas admitidos
	}
	return hex.EncodeToString(b[:])
}

// RecipeService gestiona las recetas guardadas.
type RecipeService struct {
	repo  RecipeRepository
	now   func() time.Time
	newID func() string
}

func NewRecipeService(repo RecipeRepository) *RecipeService {
	return &RecipeService{repo: repo, now: time.Now, newID: NewID}
}

// Create valida y guarda una receta nueva como versión 1.
func (s *RecipeService) Create(ctx context.Context, recipe domain.Recipe) (domain.Recipe, error) {
	if err := recipe.Validate(); err != nil {
		return recipe, err
	}
	recipe.ID, recipe.Version = s.newID(), 1
	return recipe, s.repo.Create(ctx, recipe, s.now())
}

// Update valida y guarda una nueva versión de una receta existente.
func (s *RecipeService) Update(ctx context.Context, id string, recipe domain.Recipe) (domain.Recipe, error) {
	if err := recipe.Validate(); err != nil {
		return recipe, err
	}
	current, err := s.repo.Get(ctx, id)
	if err != nil {
		return recipe, err
	}
	recipe.ID, recipe.Version = id, current.Version+1
	return recipe, s.repo.Update(ctx, recipe, s.now())
}

func (s *RecipeService) Get(ctx context.Context, id string) (domain.Recipe, error) {
	return s.repo.Get(ctx, id)
}

func (s *RecipeService) List(ctx context.Context) ([]domain.Recipe, error) {
	return s.repo.List(ctx)
}

func (s *RecipeService) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}
