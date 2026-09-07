package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteOperationRepository stores the durable ledger separately from both
// Evidence and Incident repositories.
type SQLiteOperationRepository struct{ db *sql.DB }

func NewSQLiteOperationRepository(path string) (*SQLiteOperationRepository, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("operation store database path is required")
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create operation store directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	repo := &SQLiteOperationRepository{db: db}
	if err := repo.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return repo, nil
}

func (r *SQLiteOperationRepository) migrate(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS operations (
 operation_id TEXT PRIMARY KEY, incident_id TEXT, subject_json TEXT NOT NULL, source_json TEXT NOT NULL,
 action TEXT NOT NULL, target_json TEXT NOT NULL, risk TEXT, policy_json TEXT NOT NULL,
 approval_json TEXT NOT NULL, preflight_json TEXT NOT NULL, execution_intent_json TEXT NOT NULL,
 state TEXT NOT NULL, execution_summary_json TEXT NOT NULL, verification_summary_json TEXT NOT NULL,
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL, started_at TEXT, finished_at TEXT, payload_json TEXT NOT NULL
)`,
		`CREATE INDEX IF NOT EXISTS operations_incident_updated ON operations(incident_id, updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS operations_state_updated ON operations(state, updated_at DESC)`,
		`CREATE TABLE IF NOT EXISTS operation_events (
 id INTEGER PRIMARY KEY AUTOINCREMENT, operation_id TEXT NOT NULL, type TEXT NOT NULL,
 timestamp TEXT NOT NULL, summary TEXT NOT NULL, payload_json TEXT
)`,
		`CREATE INDEX IF NOT EXISTS operation_events_operation_timestamp ON operation_events(operation_id, timestamp, id)`,
	}
	for _, statement := range statements {
		if _, err := r.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate operation store: %w", err)
		}
	}
	return nil
}

func (r *SQLiteOperationRepository) Create(ctx context.Context, operation ControlledOperation) error {
	if operation.ID == "" || operation.State == "" {
		return fmt.Errorf("operation ID and lifecycle state are required")
	}
	return r.write(ctx, operation, true)
}

func (r *SQLiteOperationRepository) Update(ctx context.Context, operation ControlledOperation) error {
	current, err := r.Get(ctx, operation.ID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current.ExecutionIntent, operation.ExecutionIntent) {
		return fmt.Errorf("operation execution intent is immutable")
	}
	if current.Source != operation.Source || current.Subject != operation.Subject || current.Action != operation.Action || !reflect.DeepEqual(current.Target, operation.Target) {
		return fmt.Errorf("operation identity is immutable")
	}
	if current.State != operation.State && !operationTransitionAllowed(current.State, operation.State) {
		return fmt.Errorf("operation lifecycle transition %s -> %s is not allowed", current.State, operation.State)
	}
	return r.write(ctx, operation, false)
}

func (r *SQLiteOperationRepository) write(ctx context.Context, operation ControlledOperation, create bool) error {
	payload, err := json.Marshal(operation)
	if err != nil {
		return err
	}
	subject, err := json.Marshal(operation.Subject)
	if err != nil {
		return err
	}
	source, err := json.Marshal(operation.Source)
	if err != nil {
		return err
	}
	target, err := json.Marshal(operation.Target)
	if err != nil {
		return err
	}
	policy, err := json.Marshal(operation.Policy)
	if err != nil {
		return err
	}
	approval, err := json.Marshal(operation.Approval)
	if err != nil {
		return err
	}
	preflight, err := json.Marshal(operation.Preflight)
	if err != nil {
		return err
	}
	intent, err := json.Marshal(operation.ExecutionIntent)
	if err != nil {
		return err
	}
	execution, err := json.Marshal(operation.Execution)
	if err != nil {
		return err
	}
	verification, err := json.Marshal(operation.Verification)
	if err != nil {
		return err
	}
	args := []interface{}{operation.Source.ID, string(subject), string(source), operation.Action, string(target), nullable(operation.Risk), string(policy), string(approval), string(preflight), string(intent), operation.State, string(execution), string(verification), operation.CreatedAt.UTC().Format(time.RFC3339Nano), operation.UpdatedAt.UTC().Format(time.RFC3339Nano), nullableTime(operation.StartedAt), nullableTime(operation.FinishedAt), string(payload)}
	if create {
		_, err = r.db.ExecContext(ctx, `INSERT INTO operations (operation_id,incident_id,subject_json,source_json,action,target_json,risk,policy_json,approval_json,preflight_json,execution_intent_json,state,execution_summary_json,verification_summary_json,created_at,updated_at,started_at,finished_at,payload_json) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, append([]interface{}{operation.ID}, args...)...)
	} else {
		_, err = r.db.ExecContext(ctx, `UPDATE operations SET incident_id=?,subject_json=?,source_json=?,action=?,target_json=?,risk=?,policy_json=?,approval_json=?,preflight_json=?,execution_intent_json=?,state=?,execution_summary_json=?,verification_summary_json=?,created_at=?,updated_at=?,started_at=?,finished_at=?,payload_json=? WHERE operation_id=?`, append(args, operation.ID)...)
	}
	return err
}

func (r *SQLiteOperationRepository) Get(ctx context.Context, id string) (*ControlledOperation, error) {
	row := r.db.QueryRowContext(ctx, `SELECT payload_json FROM operations WHERE operation_id=?`, id)
	return scanOperation(row, id)
}

func (r *SQLiteOperationRepository) List(ctx context.Context, query OperationListQuery) ([]ControlledOperation, error) {
	clauses, args := []string{"1=1"}, []interface{}{}
	if query.IncidentID != "" {
		clauses, args = append(clauses, "incident_id=?"), append(args, query.IncidentID)
	}
	if len(query.States) > 0 {
		marks := make([]string, len(query.States))
		for i, state := range query.States {
			marks[i] = "?"
			args = append(args, state)
		}
		clauses = append(clauses, "state IN ("+strings.Join(marks, ",")+")")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT payload_json FROM operations WHERE `+strings.Join(clauses, " AND ")+` ORDER BY updated_at DESC, created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	operations := []ControlledOperation{}
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var operation ControlledOperation
		if err := json.Unmarshal([]byte(payload), &operation); err != nil {
			return nil, err
		}
		operations = append(operations, operation)
	}
	return operations, rows.Err()
}

func (r *SQLiteOperationRepository) FindByIncident(ctx context.Context, incidentID string) ([]ControlledOperation, error) {
	return r.List(ctx, OperationListQuery{IncidentID: incidentID})
}

func (r *SQLiteOperationRepository) AppendEvent(ctx context.Context, operationID string, event OperationTimelineEvent) error {
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO operation_events (operation_id,type,timestamp,summary,payload_json) VALUES (?,?,?,?,?)`, operationID, event.Type, event.Timestamp.UTC().Format(time.RFC3339Nano), event.Summary, nullable(string(payload)))
	return err
}

func (r *SQLiteOperationRepository) ListEvents(ctx context.Context, operationID string) ([]OperationTimelineEvent, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,type,timestamp,summary,payload_json FROM operation_events WHERE operation_id=? ORDER BY timestamp ASC,id ASC`, operationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []OperationTimelineEvent{}
	for rows.Next() {
		var id int64
		var event OperationTimelineEvent
		var timestamp string
		var payload sql.NullString
		if err := rows.Scan(&id, &event.Type, &timestamp, &event.Summary, &payload); err != nil {
			return nil, err
		}
		event.ID, event.OperationID = fmt.Sprintf("OE-%d", id), operationID
		event.Timestamp, err = time.Parse(time.RFC3339Nano, timestamp)
		if err != nil {
			return nil, err
		}
		if payload.Valid && payload.String != "" {
			if err := json.Unmarshal([]byte(payload.String), &event.Payload); err != nil {
				return nil, err
			}
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (r *SQLiteOperationRepository) Close() error { return r.db.Close() }

func scanOperation(row rowScanner, id string) (*ControlledOperation, error) {
	var payload string
	if err := row.Scan(&payload); err != nil {
		if err == sql.ErrNoRows {
			return nil, &OperationNotFoundError{ID: id}
		}
		return nil, err
	}
	var operation ControlledOperation
	if err := json.Unmarshal([]byte(payload), &operation); err != nil {
		return nil, err
	}
	return &operation, nil
}

func nullableTime(value *time.Time) interface{} {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}
