package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type SQLiteIncidentRepository struct{ db *sql.DB }

func NewSQLiteIncidentRepository(path string) (*SQLiteIncidentRepository, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("incident store database path is required")
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create incident store directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	repo := &SQLiteIncidentRepository{db: db}
	if err := repo.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return repo, nil
}

func (r *SQLiteIncidentRepository) migrate(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS incidents (
 id TEXT PRIMARY KEY, fingerprint TEXT NOT NULL, service TEXT NOT NULL, state TEXT NOT NULL, severity TEXT NOT NULL,
 title TEXT NOT NULL, summary TEXT NOT NULL, primary_signal TEXT NOT NULL, related_release_id TEXT,
 first_observed_at TEXT NOT NULL, last_observed_at TEXT NOT NULL, mitigation_started_at TEXT,
 recovering_at TEXT, resolved_at TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, payload_json TEXT NOT NULL
)`,
		`CREATE TABLE IF NOT EXISTS incident_events (
 id INTEGER PRIMARY KEY AUTOINCREMENT, incident_id TEXT NOT NULL, type TEXT NOT NULL, timestamp TEXT NOT NULL,
 summary TEXT NOT NULL, payload_json TEXT
)`,
		`CREATE INDEX IF NOT EXISTS idx_incidents_service_state ON incidents(service, state)`,
		`CREATE INDEX IF NOT EXISTS idx_incidents_fingerprint_state ON incidents(fingerprint, state)`,
		`CREATE INDEX IF NOT EXISTS idx_incidents_updated_at ON incidents(updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_incident_events_incident_timestamp ON incident_events(incident_id, timestamp, id)`,
	}
	for _, statement := range statements {
		if _, err := r.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate incident store: %w", err)
		}
	}
	return nil
}

func (r *SQLiteIncidentRepository) Create(ctx context.Context, incident ReliabilityIncident) error {
	return r.write(ctx, incident, true)
}
func (r *SQLiteIncidentRepository) Update(ctx context.Context, incident ReliabilityIncident) error {
	return r.write(ctx, incident, false)
}
func (r *SQLiteIncidentRepository) write(ctx context.Context, incident ReliabilityIncident, create bool) error {
	payload, err := json.Marshal(incident)
	if err != nil {
		return err
	}
	summary := incidentSummary(incident)
	if create {
		_, err = r.db.ExecContext(ctx, `INSERT INTO incidents (id,fingerprint,service,state,severity,title,summary,primary_signal,related_release_id,first_observed_at,last_observed_at,mitigation_started_at,recovering_at,resolved_at,created_at,updated_at,payload_json) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, incident.ID, incident.Fingerprint, incident.Service, incident.Status, incident.Severity, incident.Title, summary, incident.PrimarySignal.Type, relatedReleaseID(incident), incident.FirstObservedAt, incident.LastObservedAt, nullable(incident.MitigationStartedAt), nullable(incident.RecoveringAt), nullable(incident.ResolvedAt), incident.CreatedAt, incident.UpdatedAt, string(payload))
	} else {
		_, err = r.db.ExecContext(ctx, `UPDATE incidents SET fingerprint=?,service=?,state=?,severity=?,title=?,summary=?,primary_signal=?,related_release_id=?,first_observed_at=?,last_observed_at=?,mitigation_started_at=?,recovering_at=?,resolved_at=?,updated_at=?,payload_json=? WHERE id=?`, incident.Fingerprint, incident.Service, incident.Status, incident.Severity, incident.Title, summary, incident.PrimarySignal.Type, relatedReleaseID(incident), incident.FirstObservedAt, incident.LastObservedAt, nullable(incident.MitigationStartedAt), nullable(incident.RecoveringAt), nullable(incident.ResolvedAt), incident.UpdatedAt, string(payload), incident.ID)
	}
	return err
}

func (r *SQLiteIncidentRepository) Get(ctx context.Context, id string) (*ReliabilityIncident, error) {
	row := r.db.QueryRowContext(ctx, `SELECT payload_json FROM incidents WHERE id=?`, id)
	return scanIncident(row)
}

func (r *SQLiteIncidentRepository) List(ctx context.Context, query IncidentListQuery) ([]ReliabilityIncident, error) {
	clauses, args := []string{"1=1"}, []interface{}{}
	if query.Service != "" {
		clauses, args = append(clauses, "service=?"), append(args, query.Service)
	}
	states := query.States
	if len(states) == 0 && !query.IncludeResolved {
		states = []IncidentStatus{IncidentStatusActive, IncidentStatusMitigating, IncidentStatusRecovering}
	}
	if len(states) > 0 {
		marks := make([]string, len(states))
		for i, state := range states {
			marks[i] = "?"
			args = append(args, state)
		}
		clauses = append(clauses, "state IN ("+strings.Join(marks, ",")+")")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT payload_json FROM incidents WHERE `+strings.Join(clauses, " AND ")+` ORDER BY updated_at DESC, created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ReliabilityIncident{}
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var item ReliabilityIncident
		if err := json.Unmarshal([]byte(payload), &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *SQLiteIncidentRepository) FindActiveByService(ctx context.Context, service string) ([]ReliabilityIncident, error) {
	return r.List(ctx, IncidentListQuery{Service: service})
}
func (r *SQLiteIncidentRepository) FindActiveByFingerprint(ctx context.Context, fingerprint string) (*ReliabilityIncident, error) {
	row := r.db.QueryRowContext(ctx, `SELECT payload_json FROM incidents WHERE fingerprint=? AND state IN ('ACTIVE','MITIGATING','RECOVERING') ORDER BY updated_at DESC LIMIT 1`, fingerprint)
	incident, err := scanIncident(row)
	if _, ok := err.(*IncidentNotFoundError); ok {
		return nil, nil
	}
	return incident, err
}

func (r *SQLiteIncidentRepository) AppendEvent(ctx context.Context, incidentID string, event IncidentTimelineEvent) error {
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, `INSERT INTO incident_events (incident_id,type,timestamp,summary,payload_json) VALUES (?,?,?,?,?)`, incidentID, event.Type, event.OccurredAt, event.Message, nullable(string(payload)))
	if err != nil {
		return err
	}
	id, _ := result.LastInsertId()
	event.ID = fmt.Sprintf("IE-%d", id)
	return nil
}
func (r *SQLiteIncidentRepository) ListEvents(ctx context.Context, incidentID string) ([]IncidentTimelineEvent, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,type,timestamp,summary,payload_json FROM incident_events WHERE incident_id=? ORDER BY timestamp ASC,id ASC`, incidentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []IncidentTimelineEvent{}
	for rows.Next() {
		var id int64
		var event IncidentTimelineEvent
		var payload sql.NullString
		if err := rows.Scan(&id, &event.Type, &event.OccurredAt, &event.Message, &payload); err != nil {
			return nil, err
		}
		event.ID = fmt.Sprintf("IE-%d", id)
		if payload.Valid && payload.String != "" {
			_ = json.Unmarshal([]byte(payload.String), &event.Payload)
		}
		events = append(events, event)
	}
	return events, rows.Err()
}
func (r *SQLiteIncidentRepository) Close() error { return r.db.Close() }

type rowScanner interface{ Scan(...interface{}) error }

func scanIncident(row rowScanner) (*ReliabilityIncident, error) {
	var payload string
	if err := row.Scan(&payload); err != nil {
		if err == sql.ErrNoRows {
			return nil, &IncidentNotFoundError{}
		}
		return nil, err
	}
	var incident ReliabilityIncident
	if err := json.Unmarshal([]byte(payload), &incident); err != nil {
		return nil, err
	}
	return &incident, nil
}
func relatedReleaseID(i ReliabilityIncident) interface{} {
	if i.RelatedRelease == nil || i.RelatedRelease.ID == "" {
		return nil
	}
	return i.RelatedRelease.ID
}
func nullable(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}
func incidentSummary(i ReliabilityIncident) string {
	if i.PrimarySignal.Reason != "" {
		return i.PrimarySignal.Reason
	}
	return i.Title
}
