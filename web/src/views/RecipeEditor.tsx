import { type FormEvent, useEffect, useState } from "react";
import { api, errorMessage } from "../api";
import { editableRecipe, parseRecipe, RECIPE_TEMPLATE } from "../recipe";
import { navigate } from "../router";
import { ErrorNote } from "../ui";

export function RecipeEditor({ id }: { id?: string }) {
  const [text, setText] = useState(id ? "" : RECIPE_TEMPLATE);
  const [version, setVersion] = useState<number | undefined>();
  const [loading, setLoading] = useState(Boolean(id));
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!id) return;
    let active = true;
    api
      .getRecipe(id)
      .then((recipe) => {
        if (!active) return;
        setText(editableRecipe(recipe));
        setVersion(recipe.version);
      })
      .catch((err) => active && setError(errorMessage(err)))
      .finally(() => active && setLoading(false));
    return () => {
      active = false;
    };
  }, [id]);

  const parsed = parseRecipe(text);

  async function save(event: FormEvent) {
    event.preventDefault();
    if (!parsed.ok) return;
    setSaving(true);
    setError(null);
    try {
      if (id) {
        await api.updateRecipe(id, parsed.recipe);
      } else {
        await api.createRecipe(parsed.recipe);
      }
      navigate("#/recipes");
    } catch (err) {
      setError(errorMessage(err));
      setSaving(false);
    }
  }

  return (
    <form onSubmit={save} className="stack">
      <h1>{id ? "Editar receta" : "Nueva receta"}</h1>
      {id && version !== undefined && (
        <p className="muted">Versión actual: {version}. Al guardar se crea la versión {version + 1}.</p>
      )}
      {loading ? (
        <p className="muted">Cargando…</p>
      ) : (
        <div className="field">
          <label className="field">
            <span>Receta en JSON</span>
            <textarea
              className="code"
              rows={20}
              spellCheck={false}
              value={text}
              onChange={(e) => setText(e.target.value)}
              aria-invalid={!parsed.ok}
            />
          </label>
          {!parsed.ok && <small className="hint hint-error">{parsed.error}</small>}
          <small className="hint">
            Tipos de campo: text, number, url. Selectores CSS. Paginación: none o next_button.
          </small>
        </div>
      )}
      <ErrorNote message={error} />
      <div className="actions">
        <button type="submit" className="primary" disabled={loading || saving || !parsed.ok}>
          {saving ? "Guardando…" : "Guardar"}
        </button>
        <a className="button" href="#/recipes">
          Cancelar
        </a>
      </div>
    </form>
  );
}
