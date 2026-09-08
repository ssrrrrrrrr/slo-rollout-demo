package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type ExecutionServiceConfig struct {
	RepoDir   string
	ReportDir string
}

type ExecutionService struct {
	cfg     ExecutionServiceConfig
	runtime ExecutionRuntime
}

type ExecutionRuntime interface {
	Descriptor() ExecutionRuntimeDescriptor
	ScriptFile() string
	ShellBin() string
	RunNoop(ctx context.Context, releaseEvidenceFile string) ([]byte, error)
}

type ExecutionRuntimeDescriptor struct {
	RuntimeID             string `json:"runtimeId"`
	RuntimeType           string `json:"runtimeType"`
	Mode                  string `json:"mode"`
	Backend               string `json:"backend"`
	Adapter               string `json:"adapter"`
	ContractVersion       string `json:"contractVersion"`
	ReadOnly              bool   `json:"readOnly"`
	WillExecute           bool   `json:"willExecute"`
	SupportsNoopExecution bool   `json:"supportsNoopExecution"`
	AutoBuildsPreview     bool   `json:"autoBuildsPreview"`
	MutatesLocalEvidence  bool   `json:"mutatesLocalEvidence"`
	DoesNotModifyCluster  bool   `json:"doesNotModifyCluster"`
	DoesNotModifyGitOps   bool   `json:"doesNotModifyGitOps"`
	DoesNotTriggerRollout bool   `json:"doesNotTriggerRollout"`
	Description           string `json:"description"`
}

func NewExecutionService(cfg Config, reportDir string) *ExecutionService {
	return &ExecutionService{
		cfg: ExecutionServiceConfig{
			RepoDir:   cfg.RepoDir,
			ReportDir: reportDir,
		},
		runtime: NewCLIExecutionRuntime(cfg.RepoDir),
	}
}

func (api *portalAPI) executionService() *ExecutionService {
	if api.executionSvc != nil {
		return api.executionSvc
	}

	return NewExecutionService(api.cfg, api.reportDir)
}

func (svc *ExecutionService) serviceContract() map[string]interface{} {
	return map[string]interface{}{
		"name":                  "s-sentinel-noop-executor-api",
		"schemaVersion":         "execution.service/v1alpha1",
		"contractVersion":       "execution.api.service/v1alpha1",
		"role":                  "policy-bound-noop-executor-control-plane",
		"readOnly":              false,
		"willExecute":           false,
		"doesNotModifyCluster":  true,
		"doesNotModifyGitOps":   true,
		"doesNotTriggerRollout": true,
	}
}

func (svc *ExecutionService) runtimePaths() map[string]interface{} {
	return map[string]interface{}{
		"repoDir":     svc.cfg.RepoDir,
		"reportDir":   svc.cfg.ReportDir,
		"scriptFile":  svc.runtime.ScriptFile(),
		"shellBinary": svc.runtime.ShellBin(),
	}
}

func (svc *ExecutionService) capabilities() map[string]interface{} {
	descriptor := svc.runtime.Descriptor()

	return map[string]interface{}{
		"runNoopExecution":        descriptor.SupportsNoopExecution,
		"autoBuildsPreview":       descriptor.AutoBuildsPreview,
		"mutatesLocalEvidence":    descriptor.MutatesLocalEvidence,
		"doesNotModifyCluster":    descriptor.DoesNotModifyCluster,
		"doesNotModifyGitOps":     descriptor.DoesNotModifyGitOps,
		"doesNotTriggerRollout":   descriptor.DoesNotTriggerRollout,
		"executionResultReader":   true,
		"evidenceRecordEmitter":   true,
		"gitopsDeliveryWorkspace": true,
		"futureExecutorAdapter":   false,
		"approvalAwareExecutor":   true,
	}
}

func (svc *ExecutionService) ControlPlaneMetadataForOperation(operation string, mutatesLocalEvidence bool) map[string]interface{} {
	descriptor := svc.runtime.Descriptor()

	return map[string]interface{}{
		"schemaVersion":             "execution.api.controlPlane/v1alpha1",
		"apiVersion":                "s-sentinel.io/execution-api/v1alpha1",
		"contractVersion":           "execution.api.response/v1alpha1",
		"generatedAt":               time.Now().Format(time.RFC3339),
		"generatedBy":               "s-sentinel-noop-executor-api",
		"operation":                 operation,
		"service":                   svc.serviceContract(),
		"runtime":                   descriptor,
		"paths":                     svc.runtimePaths(),
		"capabilities":              svc.capabilities(),
		"readOnly":                  operation != "noop",
		"willExecute":               false,
		"doesNotModifyCluster":      true,
		"doesNotModifyGitOps":       true,
		"doesNotTriggerRollout":     true,
		"mutatesLocalEvidenceFiles": mutatesLocalEvidence,
		"mutationSemantics": map[string]interface{}{
			"doesNotModifyCluster":      true,
			"doesNotModifyGitOps":       true,
			"doesNotTriggerRollout":     true,
			"mutatesLocalEvidenceFiles": mutatesLocalEvidence,
		},
	}
}

func (svc *ExecutionService) Status(ctx context.Context) map[string]interface{} {
	_ = ctx

	latestReleaseEvidenceFile, _ := svc.resolveReleaseEvidenceFile("")
	latestExecutionPreviewFile, _ := svc.findLatestReportFile("execution-preview-*.json", "execution-preview-latest.json")
	latestExecutionResultFile, _ := svc.findLatestReportFile("execution-result-*.json", "execution-result-latest.json")
	latestEvidenceRecordFile, _ := svc.findLatestReportFile("evidence-record-*.json", "evidence-record-latest.json")

	ready := false
	if scriptFile := svc.runtime.ScriptFile(); scriptFile != "" {
		if _, err := os.Stat(scriptFile); err == nil {
			ready = true
		}
	}

	body := map[string]interface{}{
		"schemaVersion":              "execution.noop.status/v1alpha1",
		"generatedAt":                time.Now().Format(time.RFC3339),
		"mode":                       svc.runtime.Descriptor().Mode,
		"service":                    svc.serviceContract(),
		"runtime":                    svc.runtime.Descriptor(),
		"paths":                      svc.runtimePaths(),
		"capabilities":               svc.capabilities(),
		"controlPlane":               svc.ControlPlaneMetadataForOperation("status", false),
		"ready":                      ready,
		"readOnly":                   true,
		"willExecute":                false,
		"doesNotModifyCluster":       true,
		"doesNotModifyGitOps":        true,
		"doesNotTriggerRollout":      true,
		"mutatesLocalEvidenceFiles":  false,
		"latestReleaseEvidenceFile":  latestReleaseEvidenceFile,
		"latestExecutionPreviewFile": latestExecutionPreviewFile,
		"latestExecutionResultFile":  latestExecutionResultFile,
		"latestEvidenceRecordFile":   latestEvidenceRecordFile,
	}

	if latestResult := svc.readJSONFile(latestExecutionResultFile); latestResult != nil {
		body["latestExecutionResult"] = latestResult
	}

	return body
}

func (svc *ExecutionService) Latest(ctx context.Context) (map[string]interface{}, error) {
	_ = ctx

	latestExecutionResultFile, err := svc.findLatestReportFile("execution-result-*.json", "execution-result-latest.json")
	if err != nil {
		return nil, err
	}

	latestExecutionResult := svc.readJSONFile(latestExecutionResultFile)
	if latestExecutionResult == nil {
		return nil, fmt.Errorf("failed to decode execution result: %s", latestExecutionResultFile)
	}

	body := map[string]interface{}{
		"schemaVersion":             "execution.noop.latest/v1alpha1",
		"generatedAt":               time.Now().Format(time.RFC3339),
		"runtime":                   svc.runtime.Descriptor(),
		"controlPlane":              svc.ControlPlaneMetadataForOperation("latest", false),
		"readOnly":                  true,
		"willExecute":               false,
		"doesNotModifyCluster":      true,
		"doesNotModifyGitOps":       true,
		"doesNotTriggerRollout":     true,
		"mutatesLocalEvidenceFiles": false,
		"latestExecutionResultFile": latestExecutionResultFile,
		"executionResult":           latestExecutionResult,
	}

	if latestEvidenceRecordFile, recordErr := svc.findLatestReportFile("evidence-record-*.json", "evidence-record-latest.json"); recordErr == nil {
		body["latestEvidenceRecordFile"] = latestEvidenceRecordFile
	}

	return body, nil
}

func (svc *ExecutionService) RunNoop(ctx context.Context, releaseID string) (map[string]interface{}, error) {
	releaseEvidenceFile, err := svc.resolveReleaseEvidenceFile(releaseID)
	if err != nil {
		return nil, err
	}

	output, err := svc.runtime.RunNoop(ctx, releaseEvidenceFile)
	if err != nil {
		return nil, err
	}

	releaseEvidence := svc.readJSONFile(releaseEvidenceFile)
	releaseEvidenceID := extractString(releaseEvidence, "releaseId")
	if releaseEvidenceID == "" {
		releaseEvidenceID = releaseID
	}

	executionResultFile := ""
	if releaseEvidence != nil {
		if artifacts, ok := releaseEvidence["artifacts"].(map[string]interface{}); ok {
			executionResultFile = strings.TrimSpace(extractString(artifacts, "executionResult"))
		}
	}
	if executionResultFile == "" && releaseEvidenceID != "" {
		candidate := filepath.Join(svc.cfg.ReportDir, "execution-result-"+releaseEvidenceID+".json")
		if _, statErr := os.Stat(candidate); statErr == nil {
			executionResultFile = candidate
		}
	}

	evidenceRecordFile := ""
	if releaseEvidenceID != "" {
		candidate := filepath.Join(svc.cfg.ReportDir, "evidence-record-"+releaseEvidenceID+".json")
		if _, statErr := os.Stat(candidate); statErr == nil {
			evidenceRecordFile = candidate
		}
	}

	body := map[string]interface{}{
		"schemaVersion":             "execution.noop.run/v1alpha1",
		"generatedAt":               time.Now().Format(time.RFC3339),
		"operation":                 "noop",
		"runtime":                   svc.runtime.Descriptor(),
		"controlPlane":              svc.ControlPlaneMetadataForOperation("noop", true),
		"readOnly":                  false,
		"willExecute":               false,
		"doesNotModifyCluster":      true,
		"doesNotModifyGitOps":       true,
		"doesNotTriggerRollout":     true,
		"mutatesLocalEvidenceFiles": true,
		"releaseEvidenceFile":       releaseEvidenceFile,
		"executionResultFile":       executionResultFile,
		"evidenceRecordFile":        evidenceRecordFile,
		"scriptOutput":              decodeExecutionOutput(output),
	}

	if releaseEvidence != nil {
		body["releaseEvidence"] = releaseEvidence
	}
	if executionResult := svc.readJSONFile(executionResultFile); executionResult != nil {
		body["executionResult"] = executionResult
	}
	if evidenceRecord := svc.readJSONFile(evidenceRecordFile); evidenceRecord != nil {
		body["evidenceRecord"] = evidenceRecord
	}

	return body, nil
}

func (svc *ExecutionService) resolveReleaseEvidenceFile(releaseID string) (string, error) {
	releaseID = strings.TrimSpace(releaseID)
	if releaseID != "" {
		path := filepath.Join(svc.cfg.ReportDir, "release-evidence-"+releaseID+".json")
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return "", fmt.Errorf("release evidence file not found for releaseId=%s", releaseID)
			}

			return "", fmt.Errorf("failed to inspect release evidence file %s: %w", path, err)
		}

		return path, nil
	}

	return svc.findLatestReportFile("release-evidence-*.json", "release-evidence-latest.json")
}

func (svc *ExecutionService) findLatestReportFile(pattern string, latestName string) (string, error) {
	path, _, found := NewArtifactLocator(svc.cfg.ReportDir).Resolve([]string{latestName}, pattern)
	if !found {
		return "", fmt.Errorf("no report files found for pattern %s", pattern)
	}
	return path, nil
}

func (svc *ExecutionService) readJSONFile(path string) map[string]interface{} {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	doc := map[string]interface{}{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil
	}

	return doc
}

func decodeExecutionOutput(output []byte) interface{} {
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return ""
	}

	doc := map[string]interface{}{}
	if err := json.Unmarshal([]byte(trimmed), &doc); err == nil {
		return doc
	}

	return trimmed
}

func extractString(object map[string]interface{}, key string) string {
	if object == nil {
		return ""
	}

	value, ok := object[key]
	if !ok || value == nil {
		return ""
	}

	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}

	return strings.TrimSpace(fmt.Sprint(value))
}

type CLIExecutionRuntime struct {
	repoDir string
}

func NewCLIExecutionRuntime(repoDir string) *CLIExecutionRuntime {
	return &CLIExecutionRuntime{repoDir: repoDir}
}

func (runtime *CLIExecutionRuntime) Descriptor() ExecutionRuntimeDescriptor {
	return ExecutionRuntimeDescriptor{
		RuntimeID:             "noop-executor-cli",
		RuntimeType:           "cli-backed-noop-executor",
		Mode:                  "noop-executor-runtime",
		Backend:               "local-file",
		Adapter:               "bash-cli",
		ContractVersion:       "execution.runtime/v1alpha1",
		ReadOnly:              false,
		WillExecute:           false,
		SupportsNoopExecution: true,
		AutoBuildsPreview:     true,
		MutatesLocalEvidence:  true,
		DoesNotModifyCluster:  true,
		DoesNotModifyGitOps:   true,
		DoesNotTriggerRollout: true,
		Description:           "Compatibility runtime that orchestrates preview-only execution evidence through scripts/run-noop-executor.sh.",
	}
}

func (runtime *CLIExecutionRuntime) ScriptFile() string {
	if scriptFile := strings.TrimSpace(os.Getenv("S_SENTINEL_NOOP_EXECUTOR_SCRIPT")); scriptFile != "" {
		return scriptFile
	}

	return filepath.Join(runtime.repoDir, "scripts", "run-noop-executor.sh")
}

func (runtime *CLIExecutionRuntime) ShellBin() string {
	if shellBin := strings.TrimSpace(os.Getenv("S_SENTINEL_BASH_BIN")); shellBin != "" {
		return shellBin
	}

	return "bash"
}

func (runtime *CLIExecutionRuntime) RunNoop(ctx context.Context, releaseEvidenceFile string) ([]byte, error) {
	scriptFile := runtime.ScriptFile()
	if _, err := os.Stat(scriptFile); err != nil {
		return nil, fmt.Errorf("noop executor script unavailable: %s: %w", scriptFile, err)
	}

	cmd := exec.CommandContext(ctx, runtime.ShellBin(), scriptFile, releaseEvidenceFile)
	cmd.Dir = runtime.repoDir

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("noop executor command failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	return stdout.Bytes(), nil
}
