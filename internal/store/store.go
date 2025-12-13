package store

import (
    "context"
    "database/sql"
    "encoding/json"
    "errors"
    "fmt"
    "time"

    _ "modernc.org/sqlite"
)

// Store wraps SQLite access for calls and jobs.
type Store struct {
    db *sql.DB
}

func Open(path string) (*Store, error) {
    db, err := sql.Open("sqlite", path)
    if err != nil {
        return nil, err
    }
    s := &Store{db: db}
    if err := s.migrate(); err != nil {
        return nil, err
    }
    return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
    stmts := []string{
        `CREATE TABLE IF NOT EXISTS calls (
            call_id TEXT PRIMARY KEY,
            filename TEXT,
            created_at TIMESTAMP,
            updated_at TIMESTAMP,
            status TEXT,
            tags_json TEXT,
            last_stage TEXT,
            last_error TEXT
        );`,
        `CREATE TABLE IF NOT EXISTS jobs (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            call_id TEXT,
            stage TEXT,
            status TEXT,
            params_json TEXT,
            idempotency_key TEXT,
            created_at TIMESTAMP,
            updated_at TIMESTAMP,
            started_at TIMESTAMP,
            finished_at TIMESTAMP
        );`,
        `CREATE UNIQUE INDEX IF NOT EXISTS idx_jobs_idem ON jobs(idempotency_key);`,
        `CREATE TABLE IF NOT EXISTS job_logs (
            job_id INTEGER,
            line TEXT,
            created_at TIMESTAMP
        );`,
    }
    for _, stmt := range stmts {
        if _, err := s.db.Exec(stmt); err != nil {
            return err
        }
    }
    return nil
}

// Call represents a call artifact stored in DB.
type Call struct {
    CallID    string    `json:"call_id"`
    Filename  string    `json:"filename"`
    Status    string    `json:"status"`
    LastStage string    `json:"last_stage"`
    LastError *string   `json:"last_error"`
    TagsJSON  *string   `json:"tags_json"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

// Job represents a pipeline job persisted to DB.
type Job struct {
    ID             int64     `json:"id"`
    CallID         string    `json:"call_id"`
    Stage          string    `json:"stage"`
    Status         string    `json:"status"`
    ParamsJSON     string    `json:"params_json"`
    IdempotencyKey string    `json:"idempotency_key"`
    CreatedAt      time.Time `json:"created_at"`
    UpdatedAt      time.Time `json:"updated_at"`
    StartedAt      *time.Time `json:"started_at"`
    FinishedAt     *time.Time `json:"finished_at"`
}

func (s *Store) UpsertCall(ctx context.Context, callID, filename, stage, status string, tags map[string]string, errMsg *string, ts time.Time) error {
    tagsJSON, _ := json.Marshal(tags)
    _, err := s.db.ExecContext(ctx, `INSERT INTO calls(call_id, filename, created_at, updated_at, status, last_stage, last_error, tags_json)
        VALUES(?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(call_id) DO UPDATE SET updated_at=excluded.updated_at, status=excluded.status, last_stage=excluded.last_stage, last_error=excluded.last_error, tags_json=excluded.tags_json`, callID, filename, ts, ts, status, stage, errMsg, string(tagsJSON))
    return err
}

func (s *Store) RecordJob(ctx context.Context, j *Job) (*Job, error) {
    if j.ParamsJSON == "" {
        j.ParamsJSON = "{}"
    }
    res, err := s.db.ExecContext(ctx, `INSERT INTO jobs(call_id, stage, status, params_json, idempotency_key, created_at, updated_at) VALUES(?,?,?,?,?,?,?)`,
        j.CallID, j.Stage, j.Status, j.ParamsJSON, j.IdempotencyKey, j.CreatedAt, j.UpdatedAt)
    if err != nil {
        return nil, err
    }
    id, _ := res.LastInsertId()
    j.ID = id
    return j, nil
}

// FetchJobByIdempotency returns existing job if present.
func (s *Store) FetchJobByIdempotency(ctx context.Context, key string) (*Job, error) {
    row := s.db.QueryRowContext(ctx, `SELECT id, call_id, stage, status, params_json, idempotency_key, created_at, updated_at, started_at, finished_at FROM jobs WHERE idempotency_key=?`, key)
    var j Job
    var started, finished sql.NullTime
    switch err := row.Scan(&j.ID, &j.CallID, &j.Stage, &j.Status, &j.ParamsJSON, &j.IdempotencyKey, &j.CreatedAt, &j.UpdatedAt, &started, &finished); err {
    case nil:
        if started.Valid {
            j.StartedAt = &started.Time
        }
        if finished.Valid {
            j.FinishedAt = &finished.Time
        }
        return &j, nil
    case sql.ErrNoRows:
        return nil, nil
    default:
        return nil, err
    }
}

func (s *Store) UpdateJobStatus(ctx context.Context, id int64, status string, ts time.Time) error {
    _, err := s.db.ExecContext(ctx, `UPDATE jobs SET status=?, updated_at=? WHERE id=?`, status, ts, id)
    return err
}

func (s *Store) MarkJobStarted(ctx context.Context, id int64, ts time.Time) error {
    _, err := s.db.ExecContext(ctx, `UPDATE jobs SET status=?, started_at=?, updated_at=? WHERE id=?`, "running", ts, ts, id)
    return err
}

func (s *Store) MarkJobFinished(ctx context.Context, id int64, status string, ts time.Time) error {
    _, err := s.db.ExecContext(ctx, `UPDATE jobs SET status=?, finished_at=?, updated_at=? WHERE id=?`, status, ts, ts, id)
    return err
}

func (s *Store) AppendJobLog(ctx context.Context, id int64, line string, ts time.Time) error {
    _, err := s.db.ExecContext(ctx, `INSERT INTO job_logs(job_id, line, created_at) VALUES(?,?,?)`, id, line, ts)
    return err
}

func (s *Store) ListCalls(ctx context.Context, limit int) ([]Call, error) {
    rows, err := s.db.QueryContext(ctx, `SELECT call_id, filename, status, last_stage, last_error, tags_json, created_at, updated_at FROM calls ORDER BY created_at DESC LIMIT ?`, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var calls []Call
    for rows.Next() {
        var c Call
        var errMsg sql.NullString
        var tags sql.NullString
        if err := rows.Scan(&c.CallID, &c.Filename, &c.Status, &c.LastStage, &errMsg, &tags, &c.CreatedAt, &c.UpdatedAt); err != nil {
            return nil, err
        }
        if errMsg.Valid {
            c.LastError = &errMsg.String
        }
        if tags.Valid {
            c.TagsJSON = &tags.String
        }
        calls = append(calls, c)
    }
    return calls, rows.Err()
}

func (s *Store) ListJobs(ctx context.Context, limit int) ([]Job, error) {
    rows, err := s.db.QueryContext(ctx, `SELECT id, call_id, stage, status, params_json, idempotency_key, created_at, updated_at, started_at, finished_at FROM jobs ORDER BY created_at DESC LIMIT ?`, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var jobs []Job
    for rows.Next() {
        var j Job
        var started, finished sql.NullTime
        if err := rows.Scan(&j.ID, &j.CallID, &j.Stage, &j.Status, &j.ParamsJSON, &j.IdempotencyKey, &j.CreatedAt, &j.UpdatedAt, &started, &finished); err != nil {
            return nil, err
        }
        if started.Valid {
            j.StartedAt = &started.Time
        }
        if finished.Valid {
            j.FinishedAt = &finished.Time
        }
        jobs = append(jobs, j)
    }
    return jobs, rows.Err()
}

func (s *Store) JobLogs(ctx context.Context, jobID int64) ([]string, error) {
    rows, err := s.db.QueryContext(ctx, `SELECT line FROM job_logs WHERE job_id=? ORDER BY created_at ASC`, jobID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var lines []string
    for rows.Next() {
        var l string
        if err := rows.Scan(&l); err != nil {
            return nil, err
        }
        lines = append(lines, l)
    }
    return lines, rows.Err()
}

var ErrConflict = errors.New("idempotent job already exists")

// InsertJobIdempotent records a job if idempotency key is new.
func (s *Store) InsertJobIdempotent(ctx context.Context, j *Job) (*Job, error) {
    existing, err := s.FetchJobByIdempotency(ctx, j.IdempotencyKey)
    if err != nil {
        return nil, err
    }
    if existing != nil {
        return existing, ErrConflict
    }
    return s.RecordJob(ctx, j)
}

// UpdateCallStage updates call record when a stage completes.
func (s *Store) UpdateCallStage(ctx context.Context, callID, stage, status string, errMsg *string, ts time.Time) error {
    tags := map[string]string{"stage": stage}
    return s.UpsertCall(ctx, callID, callID, stage, status, tags, errMsg, ts)
}

// Health returns err if DB not reachable.
func (s *Store) Health(ctx context.Context) error {
    row := s.db.QueryRowContext(ctx, `SELECT 1`)
    var v int
    if err := row.Scan(&v); err != nil {
        return fmt.Errorf("db health: %w", err)
    }
    return nil
}
