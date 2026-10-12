package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"adaptive-scraper/internal/application"
	"adaptive-scraper/internal/domain"
)

// JobRepository guarda trabajos, sus eventos y sus registros.
type JobRepository struct {
	db *sql.DB
}

const jobColumns = `id, recipe_id, recipe, url, status, max_pages, max_records, headless,
	pages_visited, record_count, warnings, error, created_at, started_at, finished_at`

func (r *JobRepository) Create(ctx context.Context, job domain.Job) error {
	recipe, warnings, err := encodeJob(job)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO jobs (`+jobColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.RecipeID, recipe, job.URL, string(job.Status), job.MaxPages, job.MaxRecords, job.Headless,
		job.PagesVisited, job.RecordCount, warnings, job.Error, formatTime(job.CreatedAt),
		formatTimePtr(job.StartedAt), formatTimePtr(job.FinishedAt))
	return err
}

// Update guarda los campos que cambian durante la vida del trabajo.
func (r *JobRepository) Update(ctx context.Context, job domain.Job) error {
	_, warnings, err := encodeJob(job)
	if err != nil {
		return err
	}
	res, err := r.db.ExecContext(ctx, `UPDATE jobs SET status = ?, pages_visited = ?, record_count = ?, warnings = ?,
		error = ?, started_at = ?, finished_at = ? WHERE id = ?`,
		string(job.Status), job.PagesVisited, job.RecordCount, warnings, job.Error,
		formatTimePtr(job.StartedAt), formatTimePtr(job.FinishedAt), job.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound("trabajo", job.ID)
	}
	return nil
}

func (r *JobRepository) Get(ctx context.Context, id string) (domain.Job, error) {
	job, err := scanJob(r.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return job, notFound("trabajo", id)
	}
	return job, err
}

// List devuelve los trabajos más recientes primero.
func (r *JobRepository) List(ctx context.Context, filter application.JobFilter) ([]domain.Job, error) {
	query, args := `SELECT `+jobColumns+` FROM jobs`, []any{}
	if filter.Status != "" {
		query += ` WHERE status = ?`
		args = append(args, string(filter.Status))
	}
	query += ` ORDER BY created_at DESC, id LIMIT ?`
	limit := filter.Limit
	if limit <= 0 {
		limit = -1
	}
	rows, err := r.db.QueryContext(ctx, query, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []domain.Job{}
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

// AppendRecords guarda registros a partir de la posición start.
func (r *JobRepository) AppendRecords(ctx context.Context, jobID string, start int, records []domain.Record) error {
	return inTx(ctx, r.db, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `INSERT INTO records (job_id, idx, data) VALUES (?, ?, ?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for i, record := range records {
			data, err := json.Marshal(record)
			if err != nil {
				return err
			}
			if _, err := stmt.ExecContext(ctx, jobID, start+i, data); err != nil {
				return err
			}
		}
		return nil
	})
}

// Records devuelve registros en orden de extracción. limit <= 0 significa todos.
func (r *JobRepository) Records(ctx context.Context, jobID string, offset, limit int) ([]domain.Record, error) {
	if limit <= 0 {
		limit = -1
	}
	rows, err := r.db.QueryContext(ctx, `SELECT data FROM records WHERE job_id = ? ORDER BY idx LIMIT ? OFFSET ?`, jobID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []domain.Record{}
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var record domain.Record
		if err := json.Unmarshal(data, &record); err != nil {
			return nil, fmt.Errorf("registro almacenado ilegible: %w", err)
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

type eventData struct {
	Page    int    `json:"page,omitempty"`
	URL     string `json:"url,omitempty"`
	Records int    `json:"records"`
	Message string `json:"message,omitempty"`
}

// AppendEvent guarda el evento y devuelve la copia con Seq asignado.
func (r *JobRepository) AppendEvent(ctx context.Context, event domain.JobEvent) (domain.JobEvent, error) {
	data, err := json.Marshal(eventData{Page: event.Page, URL: event.URL, Records: event.Records, Message: event.Message})
	if err != nil {
		return event, err
	}
	err = r.db.QueryRowContext(ctx, `INSERT INTO job_events (job_id, seq, type, at, data)
		SELECT ?, COALESCE(MAX(seq), 0) + 1, ?, ?, ? FROM job_events WHERE job_id = ?
		RETURNING seq`, event.JobID, event.Type, formatTime(event.At), data, event.JobID).Scan(&event.Seq)
	return event, err
}

// Events devuelve los eventos con Seq mayor que after, en orden.
func (r *JobRepository) Events(ctx context.Context, jobID string, after int64) ([]domain.JobEvent, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT seq, type, at, data FROM job_events WHERE job_id = ? AND seq > ? ORDER BY seq`, jobID, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []domain.JobEvent{}
	for rows.Next() {
		var (
			event domain.JobEvent
			at    string
			raw   []byte
			data  eventData
		)
		if err := rows.Scan(&event.Seq, &event.Type, &at, &raw); err != nil {
			return nil, err
		}
		if event.At, err = parseTime(at); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &data); err != nil {
			return nil, err
		}
		event.JobID, event.Page, event.URL, event.Records, event.Message = jobID, data.Page, data.URL, data.Records, data.Message
		events = append(events, event)
	}
	return events, rows.Err()
}

func encodeJob(job domain.Job) (recipe, warnings []byte, err error) {
	if recipe, err = json.Marshal(job.Recipe); err != nil {
		return nil, nil, err
	}
	if job.Warnings == nil {
		job.Warnings = []string{}
	}
	warnings, err = json.Marshal(job.Warnings)
	return recipe, warnings, err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanJob(row scanner) (domain.Job, error) {
	var (
		job                   domain.Job
		status, createdAt     string
		recipe, warnings      []byte
		startedAt, finishedAt sql.NullString
	)
	err := row.Scan(&job.ID, &job.RecipeID, &recipe, &job.URL, &status, &job.MaxPages, &job.MaxRecords, &job.Headless,
		&job.PagesVisited, &job.RecordCount, &warnings, &job.Error, &createdAt, &startedAt, &finishedAt)
	if err != nil {
		return job, err
	}
	job.Status = domain.JobStatus(status)
	if err := json.Unmarshal(recipe, &job.Recipe); err != nil {
		return job, fmt.Errorf("receta del trabajo ilegible: %w", err)
	}
	if err := json.Unmarshal(warnings, &job.Warnings); err != nil {
		return job, err
	}
	if job.CreatedAt, err = parseTime(createdAt); err != nil {
		return job, err
	}
	if job.StartedAt, err = parseTimePtr(startedAt); err != nil {
		return job, err
	}
	job.FinishedAt, err = parseTimePtr(finishedAt)
	return job, err
}
