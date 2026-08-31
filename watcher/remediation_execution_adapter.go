package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// RemediationExecutionAdapter is a thin bridge to the established Runtime
// Action pipeline. It deliberately owns no kubectl command construction or
// mutation logic: build-runtime-action-execution-result.sh remains the sole
// Runtime Action executor.
type RemediationExecutionAdapter interface {
	Available(RemediationExecutionRequest) error
	Execute(context.Context, RemediationExecutionRequest) (RuntimeActionExecutionProjection, error)
}

type RemediationExecutionRequest struct {
	ReleaseID string
	Action    string
}

type RuntimeActionExecutionProjection struct {
	ResultID       string
	Action         string
	Status         string
	StartedAt      string
	FinishedAt     string
	Reason         string
	Target         RemediationTarget
	PostState      map[string]interface{}
	ActionVerified bool
}

type RuntimeActionPipelineAdapter struct {
	repoDir   string
	reportDir string
	shellBin  string
}

func NewRuntimeActionPipelineAdapter(repoDir, reportDir string) *RuntimeActionPipelineAdapter {
	shellBin := strings.TrimSpace(os.Getenv("S_SENTINEL_BASH_BIN"))
	if shellBin == "" {
		shellBin = "bash"
	}
	return &RuntimeActionPipelineAdapter{repoDir: repoDir, reportDir: reportDir, shellBin: shellBin}
}

func (adapter *RuntimeActionPipelineAdapter) Available(request RemediationExecutionRequest) error {
	if !validRemediationReleaseID(request.ReleaseID) {
		return &RuntimeActionPipelineUnavailableError{Reason: "runtime action target release ID is invalid"}
	}
	if runtimeActionName(request.Action) == "" {
		return &RuntimeActionPipelineUnavailableError{Reason: "runtime action is unsupported"}
	}
	if _, err := os.Stat(adapter.scriptFile()); err != nil {
		return &RuntimeActionPipelineUnavailableError{Reason: "existing runtime action execution script is unavailable"}
	}
	preflight, err := adapter.loadPreflight(request)
	if err != nil {
		return err
	}
	if nestedString(preflight, "preflight", "preflightStatus") != "PREFLIGHT_PASSED" ||
		nestedString(preflight, "preflight", "eligibilityStatus") != "ELIGIBLE_FOR_CONTROLLED_EXECUTOR" ||
		!nestedBool(preflight, "preflight", "eligibleForExecution") ||
		!nestedBool(preflight, "preflight", "readyToExecute") {
		return &RuntimeActionPipelineUnavailableError{Reason: "existing runtime action preflight is not ready for controlled execution"}
	}
	return nil
}

func (adapter *RuntimeActionPipelineAdapter) Execute(ctx context.Context, request RemediationExecutionRequest) (RuntimeActionExecutionProjection, error) {
	if err := adapter.Available(request); err != nil {
		return RuntimeActionExecutionProjection{}, err
	}
	preflightFile := adapter.preflightFile(request.ReleaseID)
	started := time.Now().UTC()
	command := exec.CommandContext(ctx, adapter.shellBin, adapter.scriptFile(), preflightFile)
	command.Dir = adapter.repoDir
	output, commandErr := command.CombinedOutput()
	result, resultErr := adapter.loadResult(request, started)
	if resultErr != nil {
		if commandErr != nil {
			return RuntimeActionExecutionProjection{}, fmt.Errorf("runtime action pipeline failed: %w", commandErr)
		}
		return RuntimeActionExecutionProjection{}, resultErr
	}
	if commandErr != nil {
		result.Reason = appendReason(result.Reason, "runtime action pipeline command failed: "+strings.TrimSpace(string(output)))
		result.Status = "FAILED"
	}
	return result, nil
}

func (adapter *RuntimeActionPipelineAdapter) scriptFile() string {
	return filepath.Join(adapter.repoDir, "scripts", "build-runtime-action-execution-result.sh")
}

func (adapter *RuntimeActionPipelineAdapter) preflightFile(releaseID string) string {
	return filepath.Join(adapter.reportDir, "runtime-action-preflight-"+releaseID+".json")
}

func (adapter *RuntimeActionPipelineAdapter) resultFile(releaseID string) string {
	return filepath.Join(adapter.reportDir, "runtime-action-execution-result-"+releaseID+".json")
}

func (adapter *RuntimeActionPipelineAdapter) loadPreflight(request RemediationExecutionRequest) (map[string]interface{}, error) {
	document, err := loadRemediationJSON(adapter.preflightFile(request.ReleaseID))
	if err != nil {
		return nil, &RuntimeActionPipelineUnavailableError{Reason: "existing runtime action preflight is unavailable for this release"}
	}
	if nestedString(document, "release", "releaseId") != request.ReleaseID || nestedString(document, "request", "requestedAction") != runtimeActionName(request.Action) {
		return nil, &RuntimeActionPipelineUnavailableError{Reason: "existing runtime action preflight does not match the release recommendation"}
	}
	return document, nil
}

func (adapter *RuntimeActionPipelineAdapter) loadResult(request RemediationExecutionRequest, started time.Time) (RuntimeActionExecutionProjection, error) {
	resultFile := adapter.resultFile(request.ReleaseID)
	if _, err := os.Stat(resultFile); err != nil {
		return RuntimeActionExecutionProjection{}, fmt.Errorf("existing runtime action result was not produced for this execution")
	}
	document, err := loadRemediationJSON(resultFile)
	if err != nil {
		return RuntimeActionExecutionProjection{}, fmt.Errorf("existing runtime action result cannot be read: %w", err)
	}
	generatedAt, err := time.Parse(time.RFC3339, stringValue(document["generatedAt"]))
	if err != nil || generatedAt.Before(started.Add(-time.Second)) {
		return RuntimeActionExecutionProjection{}, fmt.Errorf("existing runtime action result is not fresh for this execution")
	}
	expectedAction := runtimeActionName(request.Action)
	if nestedString(document, "release", "releaseId") != request.ReleaseID || nestedString(document, "action", "requestedAction") != expectedAction {
		return RuntimeActionExecutionProjection{}, fmt.Errorf("existing runtime action result does not match the requested release action")
	}
	verificationStatus := nestedString(document, "postActionVerification", "verificationStatus")
	status := nestedString(document, "result", "executionStatus")
	verified := nestedBool(document, "verificationSummary", "verified")
	projection := RuntimeActionExecutionProjection{
		ResultID:       stringValue(document["runtimeActionExecutionResultId"]),
		Action:         request.Action,
		Status:         status,
		StartedAt:      nestedString(document, "action", "commandStartedAt"),
		FinishedAt:     nestedString(document, "action", "commandFinishedAt"),
		Reason:         nestedString(document, "result", "summary"),
		Target:         remediationTargetFromRuntimeResult(document),
		PostState:      nestedMap(document, "afterSnapshot"),
		ActionVerified: verified && verificationStatus == "VERIFIED",
	}
	if projection.Status == "" {
		projection.Status = "FAILED"
	}
	if projection.Reason == "" {
		projection.Reason = "runtime action result status is " + projection.Status
	}
	if !projection.ActionVerified {
		projection.Status = "FAILED"
		projection.Reason = appendReason(projection.Reason, "runtime action post-state verification is "+firstNonEmpty(verificationStatus, "not verified"))
	}
	return projection, nil
}

func runtimeActionName(action string) string {
	switch supportedRemediationAction(action) {
	case "PAUSE":
		return "PAUSE_ROLLOUT"
	case "RESUME":
		return "RESUME_ROLLOUT"
	case "PROMOTE":
		return "PROMOTE_ROLLOUT"
	case "ABORT":
		return "ABORT_ROLLOUT"
	case "ROLLBACK":
		return "ROLLBACK_ROLLOUT"
	default:
		return ""
	}
}

func validRemediationReleaseID(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.') {
			return false
		}
	}
	return true
}

func loadRemediationJSON(path string) (map[string]interface{}, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	document := map[string]interface{}{}
	if err := json.Unmarshal(bytes, &document); err != nil {
		return nil, err
	}
	return document, nil
}

func nestedMap(document map[string]interface{}, key string) map[string]interface{} {
	value, _ := document[key].(map[string]interface{})
	return value
}

func nestedString(document map[string]interface{}, keys ...string) string {
	current := document
	for index, key := range keys {
		value, ok := current[key]
		if !ok {
			return ""
		}
		if index == len(keys)-1 {
			return stringValue(value)
		}
		next, ok := value.(map[string]interface{})
		if !ok {
			return ""
		}
		current = next
	}
	return ""
}

func nestedBool(document map[string]interface{}, keys ...string) bool {
	current := document
	for index, key := range keys {
		value, ok := current[key]
		if !ok {
			return false
		}
		if index == len(keys)-1 {
			result, _ := value.(bool)
			return result
		}
		next, ok := value.(map[string]interface{})
		if !ok {
			return false
		}
		current = next
	}
	return false
}

func stringValue(value interface{}) string {
	text, _ := value.(string)
	return text
}

func remediationTargetFromRuntimeResult(document map[string]interface{}) RemediationTarget {
	target := nestedMap(document, "target")
	return RemediationTarget{ReleaseID: nestedString(document, "release", "releaseId"), Cluster: stringValue(target["cluster"]), Namespace: stringValue(target["namespace"]), Workload: stringValue(target["rolloutName"])}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func appendReason(current, addition string) string {
	if strings.TrimSpace(current) == "" {
		return addition
	}
	return current + "; " + addition
}

type RuntimeActionPipelineUnavailableError struct{ Reason string }

func (err *RuntimeActionPipelineUnavailableError) Error() string { return err.Reason }
