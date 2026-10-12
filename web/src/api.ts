// Cliente de la API HTTP. Los tipos reflejan las etiquetas JSON de
// internal/domain en el backend.

export interface FieldDefinition {
  name: string;
  type: string;
  locator: string;
  attribute?: string;
  required?: boolean;
}

export interface Recipe {
  id?: string;
  name: string;
  start_url: string;
  allowed_hosts?: string[];
  item_locator: string;
  fields: FieldDefinition[];
  pagination: { type: string; next_locator?: string };
  version?: number;
}

export type JobStatus = "queued" | "running" | "completed" | "failed" | "blocked" | "cancelled";

export interface Job {
  id: string;
  recipe_id?: string;
  recipe: Recipe;
  url: string;
  status: JobStatus;
  max_pages: number;
  max_records: number;
  headless: boolean;
  pages_visited: number;
  record_count: number;
  warnings: string[];
  error?: string;
  created_at: string;
  started_at?: string;
  finished_at?: string;
}

export interface JobEvent {
  job_id: string;
  seq: number;
  type: string;
  at: string;
  page?: number;
  url?: string;
  records: number;
  message?: string;
}

export type RecordValue = string | number | null;
export type ScrapedRecord = Record<string, RecordValue>;

export interface RecordsPage {
  fields: FieldDefinition[];
  records: ScrapedRecord[];
  total: number;
  offset: number;
  limit: number;
}

export interface JobRequest {
  url?: string;
  recipe_id?: string;
  recipe?: Recipe;
  max_pages?: number;
  max_records?: number;
  headless?: boolean;
}

export const EVENT_TYPES = [
  "job.started",
  "page.opened",
  "records.extracted",
  "job.completed",
  "job.failed",
  "job.blocked",
  "job.cancelled",
] as const;

const TERMINAL: JobStatus[] = ["completed", "failed", "blocked", "cancelled"];

export function isTerminal(status: JobStatus): boolean {
  return TERMINAL.includes(status);
}

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}

const BASE = "/api/v1";

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  let response: Response;
  try {
    response = await fetch(BASE + path, {
      method,
      // Todo POST debe llevar esta cabecera, aunque no tenga cuerpo.
      headers: method === "GET" ? undefined : { "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    throw new ApiError("No se pudo conectar con el servidor.", 0);
  }
  if (response.status === 204) {
    return undefined as T;
  }
  const data: unknown = await response.json().catch(() => null);
  if (!response.ok) {
    const message =
      data !== null && typeof data === "object" && "error" in data && typeof data.error === "string"
        ? data.error
        : `Error ${response.status}`;
    throw new ApiError(message, response.status);
  }
  return data as T;
}

export const api = {
  createJob: (job: JobRequest) => request<Job>("POST", "/jobs", job),
  listJobs: () => request<{ jobs: Job[] }>("GET", "/jobs?limit=200").then((r) => r.jobs),
  getJob: (id: string) => request<Job>("GET", `/jobs/${encodeURIComponent(id)}`),
  cancelJob: (id: string) => request<Job>("POST", `/jobs/${encodeURIComponent(id)}/cancel`),
  jobRecords: (id: string, offset: number, limit: number) =>
    request<RecordsPage>("GET", `/jobs/${encodeURIComponent(id)}/records?offset=${offset}&limit=${limit}`),
  eventsURL: (id: string) => `${BASE}/jobs/${encodeURIComponent(id)}/events`,
  exportURL: (id: string, format: "json" | "csv") => `${BASE}/jobs/${encodeURIComponent(id)}/export?format=${format}`,

  listRecipes: () => request<{ recipes: Recipe[] }>("GET", "/recipes").then((r) => r.recipes),
  getRecipe: (id: string) => request<Recipe>("GET", `/recipes/${encodeURIComponent(id)}`),
  createRecipe: (recipe: Recipe) => request<Recipe>("POST", "/recipes", recipe),
  updateRecipe: (id: string, recipe: Recipe) => request<Recipe>("PUT", `/recipes/${encodeURIComponent(id)}`, recipe),
  deleteRecipe: (id: string) => request<void>("DELETE", `/recipes/${encodeURIComponent(id)}`),
};

export function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
