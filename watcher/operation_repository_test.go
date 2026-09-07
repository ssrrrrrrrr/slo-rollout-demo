package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteOperationRepositoryCreateGetAndFindByIncident(t *testing.T) {
	repo, err := NewSQLiteOperationRepository(filepath.Join(t.TempDir(), "operations.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	op := testLedgerOperation(OperationRestartWorkload)
	op.State = OperationStatePlanned
	op.ExecutionIntent = OperationExecutionIntent{Action: op.Action, Target: op.Target, RestartAt: "2026-09-07T00:00:00Z"}
	if err := repo.Create(context.Background(), op); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(context.Background(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != op.ID || got.State != OperationStatePlanned || got.ExecutionIntent.RestartAt != op.ExecutionIntent.RestartAt {
		t.Fatalf("persisted operation = %#v", got)
	}
	byIncident, err := repo.FindByIncident(context.Background(), op.Source.ID)
	if err != nil || len(byIncident) != 1 || byIncident[0].ID != op.ID {
		t.Fatalf("find by incident = %#v err=%v", byIncident, err)
	}
}

func TestSQLiteOperationRepositoryReopenPreservesOperationIntentAndTimeline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operations.db")
	repo, err := NewSQLiteOperationRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	op := testLedgerOperation(OperationScaleWorkload)
	op.State = OperationStatePlanned
	target := int64(7)
	op.ExecutionIntent = OperationExecutionIntent{Action: op.Action, Target: op.Target, TargetReplicas: &target}
	if err := repo.Create(context.Background(), op); err != nil {
		t.Fatal(err)
	}
	if err := repo.AppendEvent(context.Background(), op.ID, OperationTimelineEvent{Type: "OPERATION_CREATED", Timestamp: time.Now().UTC(), Summary: "created"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewSQLiteOperationRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.Get(context.Background(), op.ID)
	if err != nil || got.ExecutionIntent.TargetReplicas == nil || *got.ExecutionIntent.TargetReplicas != 7 || got.State != OperationStatePlanned {
		t.Fatalf("reopened operation = %#v err=%v", got, err)
	}
	events, err := reopened.ListEvents(context.Background(), op.ID)
	if err != nil || len(events) != 1 || events[0].Type != "OPERATION_CREATED" {
		t.Fatalf("reopened timeline = %#v err=%v", events, err)
	}
}

func TestSQLiteOperationRepositoryRejectsIntentMutation(t *testing.T) {
	repo, err := NewSQLiteOperationRepository(filepath.Join(t.TempDir(), "operations.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	op := testLedgerOperation(OperationRestartWorkload)
	op.State = OperationStatePlanned
	op.ExecutionIntent = OperationExecutionIntent{Action: op.Action, Target: op.Target, RestartAt: "2026-09-07T00:00:00Z"}
	if err := repo.Create(context.Background(), op); err != nil {
		t.Fatal(err)
	}
	op.ExecutionIntent.RestartAt = "2026-09-07T01:00:00Z"
	if err := repo.Update(context.Background(), op); err == nil {
		t.Fatal("intent mutation was accepted")
	}
	_, err = repo.Get(context.Background(), "missing")
	var notFound *OperationNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("missing operation error = %v", err)
	}
}
