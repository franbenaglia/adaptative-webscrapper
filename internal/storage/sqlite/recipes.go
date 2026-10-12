package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"adaptive-scraper/internal/domain"
)

// RecipeRepository guarda recetas y el historial de sus versiones.
type RecipeRepository struct {
	db *sql.DB
}

// Create inserta la receta y su primera entrada en recipe_versions.
func (r *RecipeRepository) Create(ctx context.Context, recipe domain.Recipe, now time.Time) error {
	definition, err := json.Marshal(recipe)
	if err != nil {
		return err
	}
	return inTx(ctx, r.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO recipes (id, name, version, definition, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)`, recipe.ID, recipe.Name, recipe.Version, definition, formatTime(now), formatTime(now)); err != nil {
			return err
		}
		return insertVersion(ctx, tx, recipe, definition, now)
	})
}

// Update reemplaza la receta y añade recipe.Version al historial.
func (r *RecipeRepository) Update(ctx context.Context, recipe domain.Recipe, now time.Time) error {
	definition, err := json.Marshal(recipe)
	if err != nil {
		return err
	}
	return inTx(ctx, r.db, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE recipes SET name = ?, version = ?, definition = ?, updated_at = ? WHERE id = ?`,
			recipe.Name, recipe.Version, definition, formatTime(now), recipe.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return notFound("receta", recipe.ID)
		}
		return insertVersion(ctx, tx, recipe, definition, now)
	})
}

func insertVersion(ctx context.Context, tx *sql.Tx, recipe domain.Recipe, definition []byte, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO recipe_versions (recipe_id, version, definition, created_at) VALUES (?, ?, ?, ?)`,
		recipe.ID, recipe.Version, definition, formatTime(now))
	return err
}

func (r *RecipeRepository) Get(ctx context.Context, id string) (domain.Recipe, error) {
	var definition []byte
	err := r.db.QueryRowContext(ctx, `SELECT definition FROM recipes WHERE id = ?`, id).Scan(&definition)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Recipe{}, notFound("receta", id)
	}
	if err != nil {
		return domain.Recipe{}, err
	}
	return decodeRecipe(definition)
}

// List devuelve las recetas ordenadas por nombre.
func (r *RecipeRepository) List(ctx context.Context) ([]domain.Recipe, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT definition FROM recipes ORDER BY name COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	recipes := []domain.Recipe{}
	for rows.Next() {
		var definition []byte
		if err := rows.Scan(&definition); err != nil {
			return nil, err
		}
		recipe, err := decodeRecipe(definition)
		if err != nil {
			return nil, err
		}
		recipes = append(recipes, recipe)
	}
	return recipes, rows.Err()
}

// Versions devuelve todas las versiones guardadas, de la más antigua a la más reciente.
func (r *RecipeRepository) Versions(ctx context.Context, id string) ([]domain.Recipe, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT definition FROM recipe_versions WHERE recipe_id = ? ORDER BY version`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	versions := []domain.Recipe{}
	for rows.Next() {
		var definition []byte
		if err := rows.Scan(&definition); err != nil {
			return nil, err
		}
		recipe, err := decodeRecipe(definition)
		if err != nil {
			return nil, err
		}
		versions = append(versions, recipe)
	}
	return versions, rows.Err()
}

func (r *RecipeRepository) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM recipes WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound("receta", id)
	}
	return nil
}

func decodeRecipe(definition []byte) (domain.Recipe, error) {
	var recipe domain.Recipe
	if err := json.Unmarshal(definition, &recipe); err != nil {
		return recipe, fmt.Errorf("receta almacenada ilegible: %w", err)
	}
	return recipe, nil
}

func notFound(kind, id string) error {
	return fmt.Errorf("%s %q: %w", kind, id, domain.ErrNotFound)
}
