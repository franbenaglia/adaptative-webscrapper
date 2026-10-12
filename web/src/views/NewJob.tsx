import { type FormEvent, useEffect, useState } from "react";
import { type AIStatus, api, errorMessage, type JobRequest, type Recipe } from "../api";
import { parseRecipe, RECIPE_TEMPLATE } from "../recipe";
import { navigate } from "../router";
import { ErrorNote } from "../ui";

type Source = "ai" | "saved" | "inline";

export function NewJob({ recipeId }: { recipeId?: string }) {
  const [recipes, setRecipes] = useState<Recipe[] | null>(null);
  const [source, setSource] = useState<Source>(recipeId ? "saved" : "inline");
  const [selected, setSelected] = useState(recipeId ?? "");
  const [recipeText, setRecipeText] = useState(RECIPE_TEMPLATE);
  const [instruction, setInstruction] = useState("");
  const [ai, setAI] = useState<AIStatus | null>(null);
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

  useEffect(() => {
    let active = true;
    api
      .aiStatus()
      .then((status) => active && setAI(status))
      .catch(() => undefined); // sin estado de IA, esa opción queda desactivada
    return () => {
      active = false;
    };
  }, []);

  const aiReady = Boolean(ai?.enabled && ai.reachable && ai.model_installed);
  const parsed = parseRecipe(recipeText);
  const savedRecipe = recipes?.find((r) => r.id === selected);
  const defaultURL = source === "saved" ? savedRecipe?.start_url : parsed.ok ? parsed.recipe.start_url : undefined;
  const canSubmit =
    !submitting &&
    (source === "saved"
      ? selected !== ""
      : source === "inline"
        ? parsed.ok
        : aiReady && instruction.trim() !== "" && url.trim() !== "");

  async function submit(event: FormEvent) {
    event.preventDefault();
    const request: JobRequest = { max_pages: maxPages, max_records: maxRecords, headless };
    if (url.trim() !== "") request.url = url.trim();
    if (source === "ai") {
      request.instruction = instruction.trim();
    } else if (source === "saved") {
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
        <legend>Qué extraer</legend>
        <div className="choices">
          <label>
            <input type="radio" name="source" checked={source === "ai"} onChange={() => setSource("ai")} />
            Describirlo (IA)
          </label>
          <label>
            <input
              type="radio"
              name="source"
              checked={source === "saved"}
              disabled={!recipes || recipes.length === 0}
              onChange={() => setSource("saved")}
            />
            Receta guardada
          </label>
          <label>
            <input type="radio" name="source" checked={source === "inline"} onChange={() => setSource("inline")} />
            Receta en JSON
          </label>
        </div>

        {source === "ai" && (
          <div className="field">
            <label className="field">
              <span>Instrucción</span>
              <textarea
                rows={3}
                maxLength={2000}
                value={instruction}
                onChange={(e) => setInstruction(e.target.value)}
                placeholder="Por ejemplo: nombre, precio y disponibilidad de cada producto"
              />
            </label>
            {ai === null ? (
              <small className="hint">Comprobando el modelo…</small>
            ) : aiReady ? (
              <small className="hint">
                El modelo {ai.model} propondrá una receta para esta página. Sin GPU puede tardar un minuto o más.
                Después podrás guardarla y reutilizarla sin IA.
              </small>
            ) : (
              <small className="hint hint-error">
                {ai.message ?? "La IA no está disponible."} Mientras tanto puedes usar una receta.
              </small>
            )}
          </div>
        )}

        {source === "ai" ? null : source === "saved" ? (
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
            required={source === "ai"}
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder={(source !== "ai" && defaultURL) || "https://…"}
          />
        </label>
        <small className="hint">
          {source === "ai"
            ? "La página con el listado que quieres extraer."
            : "Opcional. Si la dejas vacía se usa la URL de la receta."}
        </small>
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
