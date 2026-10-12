import { type FormEvent, useEffect, useState } from "react";
import { api, errorMessage, type JobRequest, type Recipe } from "../api";
import { parseRecipe, RECIPE_TEMPLATE } from "../recipe";
import { navigate } from "../router";
import { ErrorNote } from "../ui";

type Source = "saved" | "inline";

export function NewJob({ recipeId }: { recipeId?: string }) {
  const [recipes, setRecipes] = useState<Recipe[] | null>(null);
  const [source, setSource] = useState<Source>(recipeId ? "saved" : "inline");
  const [selected, setSelected] = useState(recipeId ?? "");
  const [recipeText, setRecipeText] = useState(RECIPE_TEMPLATE);
  const [url, setUrl] = useState("");
  const [maxPages, setMaxPages] = useState(5);
  const [maxRecords, setMaxRecords] = useState(100);
  const [headless, setHeadless] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    let active = true;
    api
      .listRecipes()
      .then((list) => {
        if (!active) return;
        setRecipes(list);
        // Con recetas guardadas, lo habitual es reutilizar una.
        setSelected((current) => current || list[0]?.id || "");
        if (list.length > 0 && !recipeId) setSource("saved");
      })
      .catch((err) => active && setError(errorMessage(err)));
    return () => {
      active = false;
    };
  }, [recipeId]);

  const parsed = parseRecipe(recipeText);
  const savedRecipe = recipes?.find((r) => r.id === selected);
  const defaultURL = source === "saved" ? savedRecipe?.start_url : parsed.ok ? parsed.recipe.start_url : undefined;
  const canSubmit = !submitting && (source === "saved" ? selected !== "" : parsed.ok);

  async function submit(event: FormEvent) {
    event.preventDefault();
    const request: JobRequest = { max_pages: maxPages, max_records: maxRecords, headless };
    if (url.trim() !== "") request.url = url.trim();
    if (source === "saved") {
      request.recipe_id = selected;
    } else if (parsed.ok) {
      request.recipe = parsed.recipe;
    } else {
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      const job = await api.createJob(request);
      navigate(`#/jobs/${job.id}`);
    } catch (err) {
      setError(errorMessage(err));
      setSubmitting(false);
    }
  }

  return (
    <form onSubmit={submit} className="stack">
      <h1>Nueva extracción</h1>

      <fieldset>
        <legend>Receta</legend>
        <div className="choices">
          <label>
            <input
              type="radio"
              name="source"
              checked={source === "saved"}
              disabled={!recipes || recipes.length === 0}
              onChange={() => setSource("saved")}
            />
            Guardada
          </label>
          <label>
            <input type="radio" name="source" checked={source === "inline"} onChange={() => setSource("inline")} />
            JSON
          </label>
        </div>

        {source === "saved" ? (
          <label className="field">
            <span>Receta guardada</span>
            <select value={selected} onChange={(e) => setSelected(e.target.value)}>
              {(recipes ?? []).map((r) => (
                <option key={r.id} value={r.id}>
                  {r.name} (v{r.version})
                </option>
              ))}
            </select>
          </label>
        ) : (
          <div className="field">
            <label className="field">
              <span>Receta en JSON</span>
              <textarea
                className="code"
                rows={14}
                spellCheck={false}
                value={recipeText}
                onChange={(e) => setRecipeText(e.target.value)}
                aria-invalid={!parsed.ok}
              />
            </label>
            {!parsed.ok && <small className="hint hint-error">{parsed.error}</small>}
            {recipes && recipes.length === 0 && (
              <small className="hint">Aún no hay recetas guardadas. Podrás guardar esta desde el trabajo.</small>
            )}
          </div>
        )}
      </fieldset>

      <div className="field">
        <label className="field">
          <span>URL inicial</span>
          <input
            type="url"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder={defaultURL ?? "https://…"}
          />
        </label>
        <small className="hint">Opcional. Si la dejas vacía se usa la URL de la receta.</small>
      </div>

      <div className="row">
        <label className="field">
          <span>Páginas como máximo</span>
          <input type="number" min={1} max={1000} required value={maxPages} onChange={(e) => setMaxPages(Number(e.target.value))} />
        </label>
        <label className="field">
          <span>Registros como máximo</span>
          <input
            type="number"
            min={1}
            max={100000}
            required
            value={maxRecords}
            onChange={(e) => setMaxRecords(Number(e.target.value))}
          />
        </label>
      </div>

      <label className="check">
        <input type="checkbox" checked={headless} onChange={(e) => setHeadless(e.target.checked)} />
        Navegador sin ventana (headless)
      </label>

      <ErrorNote message={error} />

      <div>
        <button type="submit" className="primary" disabled={!canSubmit}>
          {submitting ? "Iniciando…" : "Iniciar extracción"}
        </button>
      </div>
    </form>
  );
}
