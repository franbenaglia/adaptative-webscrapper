import type { JobStatus } from "./api";

const STATUS_LABELS: Record<JobStatus, string> = {
  queued: "En cola",
  running: "En curso",
  completed: "Completado",
  failed: "Fallido",
  blocked: "Bloqueado",
  cancelled: "Cancelado",
};

export function StatusBadge({ status }: { status: JobStatus }) {
  return <span className={`badge badge-${status}`}>{STATUS_LABELS[status] ?? status}</span>;
}

export function ErrorNote({ message }: { message: string | null }) {
  if (!message) return null;
  return (
    <p className="note note-error" role="alert">
      {message}
    </p>
  );
}

export function formatDate(iso: string | undefined): string {
  if (!iso) return "—";
  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? iso : date.toLocaleString();
}
