CREATE TABLE recipes (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    version    INTEGER NOT NULL,
    definition TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE recipe_versions (
    recipe_id  TEXT NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    version    INTEGER NOT NULL,
    definition TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (recipe_id, version)
);

-- recipe_id no es clave foránea: el historial sobrevive al borrado de la
-- receta gracias a la copia guardada en la columna recipe.
CREATE TABLE jobs (
    id            TEXT PRIMARY KEY,
    recipe_id     TEXT NOT NULL DEFAULT '',
    recipe        TEXT NOT NULL,
    url           TEXT NOT NULL,
    status        TEXT NOT NULL,
    max_pages     INTEGER NOT NULL,
    max_records   INTEGER NOT NULL,
    headless      INTEGER NOT NULL,
    pages_visited INTEGER NOT NULL DEFAULT 0,
    record_count  INTEGER NOT NULL DEFAULT 0,
    warnings      TEXT NOT NULL DEFAULT '[]',
    error         TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL,
    started_at    TEXT,
    finished_at   TEXT
);

CREATE INDEX jobs_created_at ON jobs (created_at DESC);
CREATE INDEX jobs_status ON jobs (status);

CREATE TABLE job_events (
    job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    seq    INTEGER NOT NULL,
    type   TEXT NOT NULL,
    at     TEXT NOT NULL,
    data   TEXT NOT NULL,
    PRIMARY KEY (job_id, seq)
);

CREATE TABLE records (
    job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    idx    INTEGER NOT NULL,
    data   TEXT NOT NULL,
    PRIMARY KEY (job_id, idx)
);
