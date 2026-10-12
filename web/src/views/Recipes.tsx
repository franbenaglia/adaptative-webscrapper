import { useCallback, useEffect, useState } from "react";
import { api, errorMessage, type Recipe } from "../api";
import { ErrorNote } from "../ui";

export function Recipes() {
  const [recipes, setRecipes] = useState<Recipe[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    api
      .listRecipes()
      .then(setRecipes)
      .catch((err) => setError(errorMessage(err)));
  }, []);

  useEffect(load, [load]);

  async function remove(recipe: Recipe) {
    if (!recipe.id || !window.confirm(`¿Borrar la receta «${recipe.name}»? Los trabajos ya ejecutados se conservan.`)) {
      return;
    }
    try {
      await api.deleteRecipe(recipe.id);
      setError(null);
      load();
    } catch (err) {
      setError(errorMessage(err));
    }
  }

  return (
    <div className="stack">
      <div className="heading">
        <h1>Recetas</h1>
        <a className="button primary" href="#/recipes/new">
          Nueva receta
        </a>
      </div>
      <ErrorNote message={error} />
      {recipes === null && !error && <p className="muted">Cargando…</p>}
      {recipes?.length === 0 && <p className="muted">Todavía no hay recetas guardadas.</p>}
      {recipes && recipes.length > 0 && (
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Nombre</th>
                <th className="num">Versión</th>
                <th className="num">Campos</th>
                <th>Acciones</th>
              </tr>
            </thead>
            <tbody>
              {recipes.map((recipe) => (
                <tr key={recipe.id}>
                  <td>
                    <a href={`#/recipes/${recipe.id}`}>{recipe.name}</a>
                    <div className="muted small break">{recipe.start_url}</div>
                  </td>
                  <td className="num">{recipe.version}</td>
                  <td className="num">{recipe.fields.length}</td>
                  <td>
                    <div className="actions">
                      <a className="button" href={`#/?recipe=${recipe.id}`}>
                        Ejecutar
                      </a>
                      <a className="button" href={`#/recipes/${recipe.id}`}>
                        Editar
                      </a>
                      <button type="button" className="danger" onClick={() => remove(recipe)}>
                        Borrar
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
