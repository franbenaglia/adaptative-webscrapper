package api

import (
	"net/http"

	"adaptive-scraper/internal/domain"
)

func (s *Server) listRecipes(w http.ResponseWriter, r *http.Request) {
	recipes, err := s.Recipes.List(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recipes": recipes})
}

func (s *Server) createRecipe(w http.ResponseWriter, r *http.Request) {
	var recipe domain.Recipe
	if err := decodeJSON(w, r, &recipe, false); err != nil {
		s.writeError(w, r, err)
		return
	}
	saved, err := s.Recipes.Create(r.Context(), recipe)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/recipes/"+saved.ID)
	writeJSON(w, http.StatusCreated, saved)
}

func (s *Server) getRecipe(w http.ResponseWriter, r *http.Request) {
	recipe, err := s.Recipes.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, recipe)
}

func (s *Server) updateRecipe(w http.ResponseWriter, r *http.Request) {
	var recipe domain.Recipe
	if err := decodeJSON(w, r, &recipe, false); err != nil {
		s.writeError(w, r, err)
		return
	}
	saved, err := s.Recipes.Update(r.Context(), r.PathValue("id"), recipe)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) deleteRecipe(w http.ResponseWriter, r *http.Request) {
	if err := s.Recipes.Delete(r.Context(), r.PathValue("id")); err != nil {
		s.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
