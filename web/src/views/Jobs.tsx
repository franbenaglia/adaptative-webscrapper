import { useEffect, useState } from "react";
import { api, errorMessage, isTerminal, type Job, jobTitle } from "../api";
import { ErrorNote, formatDate, StatusBadge } from "../ui";

const REFRESH_MS = 3000;

export function Jobs() {
  const [jobs, setJobs] = useState<Job[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    let timer: number | undefined;
    const load = async () => {
      try {
        const list = await api.listJobs();
        if (!active) return;
        setJobs(list);
        setError(null);
        // Solo se sigue consultando mientras haya trabajos sin terminar.
        if (list.some((job) => !isTerminal(job.status))) {
          timer = window.setTimeout(load, REFRESH_MS);
        }
      } catch (err) {
        if (active) setError(errorMessage(err));
      }
    };
    void load();
    return () => {
      active = false;
      window.clearTimeout(timer);
    };
  }, []);

  return (
    <div className="stack">
      <h1>Trabajos</h1>
      <ErrorNote message={error} />
      {jobs === null && !error && <p className="muted">Cargando…</p>}
      {jobs?.length === 0 && (
        <p className="muted">
          Todavía no hay trabajos. <a href="#/">Inicia una extracción</a>.
        </p>
      )}
      {jobs && jobs.length > 0 && (
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Receta</th>
                <th>Estado</th>
                <th className="num">Registros</th>
                <th className="num">Páginas</th>
                <th>Creado</th>
              </tr>
            </thead>
            <tbody>
              {jobs.map((job) => (
                <tr key={job.id}>
                  <td>
                    <a href={`#/jobs/${job.id}`}>{jobTitle(job)}</a>
                    <div className="muted small break">{job.url}</div>
                  </td>
                  <td>
                    <StatusBadge status={job.status} />
                  </td>
                  <td className="num">{job.record_count}</td>
                  <td className="num">{job.pages_visited}</td>
                  <td>{formatDate(job.created_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
