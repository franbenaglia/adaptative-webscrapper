import { useCallback, useEffect, useRef, useState } from "react";
import { api, errorMessage, EVENT_TYPES, isTerminal, type Job, type JobEvent, type RecordsPage } from "../api";
import { navigate } from "../router";
import { ErrorNote, formatDate, StatusBadge } from "../ui";

const PAGE_SIZE = 50;

function describe(event: JobEvent): string {
  switch (event.type) {
    case "job.started":
      return "Trabajo iniciado";
    case "page.opened":
      return `Página ${event.page} abierta: ${event.url}`;
    case "records.extracted":
      return `Página ${event.page}: ${event.records} registros acumulados`;
    case "job.completed":
      return `Completado con ${event.records} registros`;
    case "job.failed":
      return `Falló: ${event.message ?? "error desconocido"}`;
    case "job.blocked":
      return `Bloqueado: ${event.message ?? "el sitio impidió el acceso"}`;
    case "job.cancelled":
      return "Cancelado";
    default:
      return event.type;
  }
}

export function JobDetail({ id }: { id: string }) {
  const [job, setJob] = useState<Job | null>(null);
  const [events, setEvents] = useState<JobEvent[]>([]);
  const [page, setPage] = useState<RecordsPage | null>(null);
  const [offset, setOffset] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [recipeSaved, setRecipeSaved] = useState(false);
  const offsetRef = useRef(0);
  offsetRef.current = offset;

  const refresh = useCallback(async () => {
    try {
      const [current, records] = await Promise.all([api.getJob(id), api.jobRecords(id, offsetRef.current, PAGE_SIZE)]);
      setJob(current);
      setPage(records);
    } catch (err) {
      setError(errorMessage(err));
    }
  }, [id]);

  useEffect(() => {
    void refresh();
  }, [refresh, offset]);

  // El servidor reenvía primero los eventos ya guardados y luego los nuevos;
  // tras el evento final cierra la conexión, y aquí se cierra para que el
  // navegador no intente reconectar.
  useEffect(() => {
    const source = new EventSource(api.eventsURL(id));
    const onEvent = (message: MessageEvent<string>) => {
      const event = JSON.parse(message.data) as JobEvent;
      setEvents((current) => (current.some((e) => e.seq === event.seq) ? current : [...current, event]));
      if (event.type.startsWith("job.") && event.type !== "job.started") {
        source.close();
      }
      if (event.type !== "page.opened") {
        void refresh();
      }
    };
    for (const type of EVENT_TYPES) {
      source.addEventListener(type, onEvent);
    }
    return () => source.close();
  }, [id, refresh]);

  async function run(action: () => Promise<void>) {
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      await action();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  if (!job) {
    return (
      <div className="stack">
        <h1>Trabajo</h1>
        {error ? <ErrorNote message={error} /> : <p className="muted">Cargando…</p>}
      </div>
    );
  }

  const done = isTerminal(job.status);
  const fields = page?.fields ?? job.recipe.fields;
  const total = page?.total ?? 0;

  return (
    <div className="stack">
      <div className="heading">
        <h1>{job.recipe.name}</h1>
        <StatusBadge status={job.status} />
      </div>

      <dl className="summary">
        <div>
          <dt>Registros</dt>
          <dd>{job.record_count}</dd>
        </div>
        <div>
          <dt>Páginas</dt>
          <dd>
            {job.pages_visited} de {job.max_pages} como máximo
          </dd>
        </div>
        <div>
          <dt>Creado</dt>
          <dd>{formatDate(job.created_at)}</dd>
        </div>
        <div>
          <dt>Terminado</dt>
          <dd>{formatDate(job.finished_at)}</dd>
        </div>
        <div className="wide">
          <dt>URL</dt>
          <dd className="break">{job.url}</dd>
        </div>
      </dl>

      {job.error && <p className={`note ${job.status === "cancelled" ? "note-info" : "note-error"}`}>{job.error}</p>}
      {job.warnings.length > 0 && (
        <ul className="note note-warning">
          {job.warnings.map((warning) => (
            <li key={warning}>{warning}</li>
          ))}
        </ul>
      )}
      <ErrorNote message={error} />
      {notice && <p className="note note-info">{notice}</p>}

      <div className="actions">
        {!done && (
          <button type="button" disabled={busy} onClick={() => run(async () => void (await api.cancelJob(id)))}>
            Cancelar
          </button>
        )}
        <a className={`button${job.record_count === 0 ? " disabled" : ""}`} href={api.exportURL(id, "csv")}>
          Exportar CSV
        </a>
        <a className={`button${job.record_count === 0 ? " disabled" : ""}`} href={api.exportURL(id, "json")}>
          Exportar JSON
        </a>
        {done && (
          <button
            type="button"
            disabled={busy}
            onClick={() =>
              run(async () => {
                const again = await api.createJob({
                  url: job.url,
                  max_pages: job.max_pages,
                  max_records: job.max_records,
                  headless: job.headless,
                  ...(job.recipe_id ? { recipe_id: job.recipe_id } : { recipe: job.recipe }),
                });
                navigate(`#/jobs/${again.id}`);
              })
            }
          >
            Ejecutar de nuevo
          </button>
        )}
        {!job.recipe_id && !recipeSaved && (
          <button
            type="button"
            disabled={busy}
            onClick={() =>
              run(async () => {
                const saved = await api.createRecipe(job.recipe);
                setRecipeSaved(true);
                setNotice(`Receta guardada como «${saved.name}». Ya puedes reutilizarla desde Recetas.`);
              })
            }
          >
            Guardar receta
          </button>
        )}
      </div>

      <section className="stack">
        <h2>Progreso</h2>
        {events.length === 0 ? (
          <p className="muted">{done ? "Sin eventos registrados." : "Esperando a que empiece…"}</p>
        ) : (
          <ol className="events" aria-live="polite">
            {events.map((event) => (
              <li key={event.seq}>
                <time>{new Date(event.at).toLocaleTimeString()}</time>
                <span>{describe(event)}</span>
              </li>
            ))}
          </ol>
        )}
      </section>

      <section className="stack">
        <h2>Registros</h2>
        {total === 0 ? (
          <p className="muted">{done ? "No se extrajo ningún registro." : "Todavía no hay registros."}</p>
        ) : (
          <>
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th className="num">#</th>
                    {fields.map((field) => (
                      <th key={field.name} className={field.type === "number" ? "num" : undefined}>
                        {field.name}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {(page?.records ?? []).map((record, index) => (
                    <tr key={offset + index}>
                      <td className="num muted">{offset + index + 1}</td>
                      {fields.map((field) => {
                        const value = record[field.name];
                        return (
                          <td key={field.name} className={field.type === "number" ? "num" : undefined}>
                            {value === null || value === undefined ? (
                              <span className="muted">—</span>
                            ) : field.type === "url" ? (
                              <a href={String(value)} target="_blank" rel="noreferrer noopener">
                                {String(value)}
                              </a>
                            ) : (
                              String(value)
                            )}
                          </td>
                        );
                      })}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="pager">
              <span className="muted">
                {offset + 1}–{Math.min(offset + PAGE_SIZE, total)} de {total}
              </span>
              <button type="button" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}>
                Anteriores
              </button>
              <button type="button" disabled={offset + PAGE_SIZE >= total} onClick={() => setOffset(offset + PAGE_SIZE)}>
                Siguientes
              </button>
            </div>
          </>
        )}
      </section>

      <details>
        <summary>Receta utilizada</summary>
        <pre className="code">{JSON.stringify(job.recipe, null, 2)}</pre>
      </details>
    </div>
  );
}
