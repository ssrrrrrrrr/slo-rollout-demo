package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	watchpkg "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type Target struct {
	Namespace string `yaml:"namespace"`
	Rollout   string `yaml:"rollout"`
}

type Config struct {
	Namespace string   `yaml:"namespace"`
	Rollout   string   `yaml:"rollout"`
	Targets   []Target `yaml:"targets"`

	Interval                      string `yaml:"interval"`
	Mode                          string `yaml:"mode"`
	RepoDir                       string `yaml:"repoDir"`
	ReportDir                     string `yaml:"reportDir"`
	StateFile                     string `yaml:"stateFile"`
	EvidenceStoreDB               string `yaml:"evidenceStoreDB"`
	EvidenceStoreScriptFile       string `yaml:"evidenceStoreScriptFile"`
	EvidenceStorePython           string `yaml:"evidenceStorePython"`
	EvidenceStoreRefreshStateFile string `yaml:"evidenceStoreRefreshStateFile"`
	OllamaURL                     string `yaml:"ollamaUrl"`
	Model                         string `yaml:"model"`
	PrometheusURL                 string `yaml:"prometheusUrl"`
	HealthAddr                    string `yaml:"healthAddr"`
}

type WatchEvent struct {
	Key                   string
	Namespace             string
	RolloutName           string
	RolloutPhase          string
	RolloutMessage        string
	RolloutAbort          bool
	StableReplicaSet      string
	CurrentDesiredVersion string
	AnalysisRunName       string
	AnalysisRunPhase      string
	FailedMetric          string
	FailedMetrics         []string
	AnalysisRunMetrics    []MetricResult
	Reason                string
}

type State struct {
	Processed []string `json:"processed"`
}

var watcherReady atomic.Bool

var (
	rolloutGVR = schema.GroupVersionResource{
		Group:    "argoproj.io",
		Version:  "v1alpha1",
		Resource: "rollouts",
	}

	analysisRunGVR = schema.GroupVersionResource{
		Group:    "argoproj.io",
		Version:  "v1alpha1",
		Resource: "analysisruns",
	}
)

func defaultConfig() Config {
	return Config{
		Interval:            "10s",
		RepoDir:             "/root/slo-rollout-demo",
		StateFile:           "/root/slo-rollout-demo/docs/release-reports/go-rollout-watcher-state.json",
		EvidenceStorePython: "python3",
		OllamaURL:           "http://192.168.30.1:11434",
		Model:               "qwen2.5:0.5b",
		PrometheusURL:       "http://prometheus-stack-kube-prom-prometheus.monitoring.svc.cluster.local:9090",
		HealthAddr:          ":8080",
		Targets: []Target{
			{
				Namespace: "slo-rollout",
				Rollout:   "demo-app",
			},
		},
	}
}

func loadConfig(path string) (Config, error) {
	cfg := defaultConfig()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return cfg, err
		}

		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return cfg, err
		}
	}

	def := defaultConfig()

	if cfg.Interval == "" {
		cfg.Interval = def.Interval
	}
	if cfg.RepoDir == "" {
		cfg.RepoDir = def.RepoDir
	}
	if cfg.ReportDir == "" {
		cfg.ReportDir = filepath.Join(cfg.RepoDir, "docs", "release-reports")
	}
	if cfg.EvidenceStoreDB == "" {
		cfg.EvidenceStoreDB = filepath.Join(os.TempDir(), "s-sentinel-evidence-store", "portal-evidence-store.db")
	}
	if cfg.EvidenceStoreScriptFile == "" {
		cfg.EvidenceStoreScriptFile = filepath.Join(cfg.RepoDir, "scripts", "evidence-store.py")
	}
	if cfg.EvidenceStorePython == "" {
		cfg.EvidenceStorePython = def.EvidenceStorePython
	}
	if cfg.EvidenceStoreRefreshStateFile == "" {
		ext := filepath.Ext(cfg.EvidenceStoreDB)
		if ext == "" {
			cfg.EvidenceStoreRefreshStateFile = cfg.EvidenceStoreDB + "-refresh.json"
		} else {
			cfg.EvidenceStoreRefreshStateFile = strings.TrimSuffix(cfg.EvidenceStoreDB, ext) + "-refresh.json"
		}
	}
	if cfg.StateFile == "" {
		cfg.StateFile = def.StateFile
	}
	if cfg.OllamaURL == "" {
		cfg.OllamaURL = def.OllamaURL
	}
	if cfg.Model == "" {
		cfg.Model = def.Model
	}
	if cfg.PrometheusURL == "" {
		cfg.PrometheusURL = def.PrometheusURL
	}
	if cfg.HealthAddr == "" {
		cfg.HealthAddr = def.HealthAddr
	}

	// 兼容旧配置：namespace + rollout
	if len(cfg.Targets) == 0 && cfg.Namespace != "" && cfg.Rollout != "" {
		cfg.Targets = []Target{
			{
				Namespace: cfg.Namespace,
				Rollout:   cfg.Rollout,
			},
		}
	}

	// 如果没有任何 target，就使用默认 target
	if len(cfg.Targets) == 0 {
		cfg.Targets = def.Targets
	}

	return cfg, nil
}

func startHealthServer(addr string, cfg Config) {
	mux := http.NewServeMux()

	registerPortalAPIHandlers(mux, cfg)

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	mux.HandleFunc("/metrics", writeMetrics)

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if watcherReady.Load() {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ready\n"))
			return
		}

		http.Error(w, "not ready", http.StatusServiceUnavailable)
	})

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("health server started: addr=%s", addr)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("health server failed: %v", err)
	}
}

func buildKubeConfig() (*rest.Config, error) {
	if kubeconfig := os.Getenv("KUBECONFIG"); kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}

	home, err := os.UserHomeDir()
	if err == nil {
		kubeconfig := filepath.Join(home, ".kube", "config")
		if _, statErr := os.Stat(kubeconfig); statErr == nil {
			return clientcmd.BuildConfigFromFlags("", kubeconfig)
		}
	}

	return rest.InClusterConfig()
}

func loadState(path string) State {
	data, err := os.ReadFile(path)
	if err != nil {
		return State{Processed: []string{}}
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{Processed: []string{}}
	}

	return s
}

func saveState(path string, s State) {
	data, _ := json.MarshalIndent(s, "", "  ")
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	_ = os.WriteFile(path, data, 0644)
}

func contains(list []string, item string) bool {
	for _, v := range list {
		if v == item {
			return true
		}
	}
	return false
}

func tailKeep(list []string, max int) []string {
	if len(list) <= max {
		return list
	}
	return list[len(list)-max:]
}

func getString(obj *unstructured.Unstructured, fields ...string) string {
	v, found, _ := unstructured.NestedString(obj.Object, fields...)
	if !found {
		return ""
	}
	return v
}

func getBool(obj *unstructured.Unstructured, fields ...string) bool {
	v, found, _ := unstructured.NestedBool(obj.Object, fields...)
	if !found {
		return false
	}
	return v
}

func getInt64(obj *unstructured.Unstructured, fields ...string) int64 {
	v, found, _ := unstructured.NestedInt64(obj.Object, fields...)
	if !found {
		return 0
	}
	return v
}

func latestAnalysisRun(ctx context.Context, client dynamic.Interface, target Target) (*unstructured.Unstructured, error) {
	list, err := client.Resource(analysisRunGVR).Namespace(target.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var latest *unstructured.Unstructured
	var latestTime time.Time

	for i := range list.Items {
		item := &list.Items[i]
		name := item.GetName()

		matched := strings.HasPrefix(name, target.Rollout+"-")

		for _, owner := range item.GetOwnerReferences() {
			if owner.Kind == "Rollout" && owner.Name == target.Rollout {
				matched = true
				break
			}
		}

		if !matched {
			continue
		}

		t := item.GetCreationTimestamp().Time
		if latest == nil || t.After(latestTime) {
			latest = item
			latestTime = t
		}
	}

	return latest, nil
}

func buildEvent(target Target, rollout *unstructured.Unstructured, ar *unstructured.Unstructured) WatchEvent {
	phase := getString(rollout, "status", "phase")
	message := getString(rollout, "status", "message")
	abort := getBool(rollout, "status", "abort")

	observedGeneration := getInt64(rollout, "status", "observedGeneration")
	if observedGeneration == 0 {
		observedGeneration = rollout.GetGeneration()
	}

	stableRS := getString(rollout, "status", "stableRS")
	currentDesiredVersion := getTemplateVersion(rollout)
	failedMetric := extractFailedMetricFromMessage(message)
	failedMetrics := []string{}
	failedMetrics = appendUniqueString(failedMetrics, failedMetric)
	analysisMetrics := []MetricResult{}

	arName := "none"
	arPhase := "none"

	if ar != nil {
		arName = ar.GetName()
		arPhase = getString(ar, "status", "phase")

		metrics, arFailedMetrics := extractAnalysisMetrics(ar)
		analysisMetrics = metrics

		for _, metric := range arFailedMetrics {
			failedMetrics = appendUniqueString(failedMetrics, metric)
		}

		if failedMetric == "unknown" && len(arFailedMetrics) > 0 {
			failedMetric = arFailedMetrics[0]
		}
	}

	reasons := []string{}

	if strings.EqualFold(phase, "Degraded") {
		reasons = append(reasons, "rollout phase is Degraded")
	}

	if abort {
		reasons = append(reasons, "rollout abort is true")
	}

	if strings.EqualFold(arPhase, "Failed") || strings.EqualFold(arPhase, "Error") {
		reasons = append(reasons, fmt.Sprintf("analysisrun %s phase is %s", arName, arPhase))
	}

	msgLower := strings.ToLower(message)
	if strings.Contains(msgLower, "abort") || strings.Contains(msgLower, "failed") {
		reasons = append(reasons, "rollout message contains failure signal")
	}

	key := fmt.Sprintf(
		"%s/%s:%s:%d:%s:%s:%s:%t",
		target.Namespace,
		target.Rollout,
		rollout.GetUID(),
		observedGeneration,
		arName,
		arPhase,
		phase,
		abort,
	)

	return WatchEvent{
		Key:                   key,
		Namespace:             target.Namespace,
		RolloutName:           target.Rollout,
		RolloutPhase:          phase,
		RolloutMessage:        message,
		RolloutAbort:          abort,
		StableReplicaSet:      stableRS,
		CurrentDesiredVersion: currentDesiredVersion,
		AnalysisRunName:       arName,
		AnalysisRunPhase:      arPhase,
		FailedMetric:          failedMetric,
		FailedMetrics:         failedMetrics,
		AnalysisRunMetrics:    analysisMetrics,
		Reason:                strings.Join(reasons, "; "),
	}
}

func getTemplateVersion(rollout *unstructured.Unstructured) string {
	version := getString(rollout, "spec", "template", "metadata", "labels", "version")
	if version != "" {
		return version
	}

	containers, found, _ := unstructured.NestedSlice(rollout.Object, "spec", "template", "spec", "containers")
	if !found {
		return "unknown"
	}

	for _, c := range containers {
		container, ok := c.(map[string]interface{})
		if !ok {
			continue
		}

		envList, ok := container["env"].([]interface{})
		if !ok {
			continue
		}

		for _, envItem := range envList {
			env, ok := envItem.(map[string]interface{})
			if !ok {
				continue
			}

			name, _ := env["name"].(string)
			value, _ := env["value"].(string)

			if name == "RELEASE_TAG" || name == "APP_VERSION" {
				if value != "" {
					return value
				}
			}
		}
	}

	return "unknown"
}

func int64FromInterface(v interface{}) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	case json.Number:
		n, _ := t.Int64()
		return n
	default:
		return 0
	}
}

func extractFailedMetricFromMessage(message string) string {
	startToken := `Metric "`
	start := strings.Index(message, startToken)
	if start < 0 {
		return "unknown"
	}

	remain := message[start+len(startToken):]
	end := strings.Index(remain, `"`)
	if end < 0 {
		return "unknown"
	}

	metric := remain[:end]
	if metric == "" {
		return "unknown"
	}

	return metric
}

func appendUniqueString(list []string, item string) []string {
	if item == "" || item == "unknown" {
		return list
	}

	for _, existing := range list {
		if existing == item {
			return list
		}
	}

	return append(list, item)
}

func extractAnalysisMetrics(ar *unstructured.Unstructured) ([]MetricResult, []string) {
	results := []MetricResult{}
	failedMetrics := []string{}

	rawResults, found, _ := unstructured.NestedSlice(ar.Object, "status", "metricResults")
	if !found {
		return results, failedMetrics
	}

	for _, raw := range rawResults {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}

		name, _ := item["name"].(string)
		phase, _ := item["phase"].(string)
		message, _ := item["message"].(string)

		value := ""

		if measurements, ok := item["measurements"].([]interface{}); ok && len(measurements) > 0 {
			last := measurements[len(measurements)-1]
			if m, ok := last.(map[string]interface{}); ok {
				value, _ = m["value"].(string)
			}
		}

		result := MetricResult{
			Name:         name,
			Phase:        phase,
			Message:      message,
			Value:        value,
			Successful:   int64FromInterface(item["successful"]),
			Failed:       int64FromInterface(item["failed"]),
			Inconclusive: int64FromInterface(item["inconclusive"]),
			Error:        int64FromInterface(item["error"]),
		}

		if strings.EqualFold(phase, "Failed") {
			failedMetrics = appendUniqueString(failedMetrics, name)
		}

		results = append(results, result)
	}

	return results, failedMetrics
}

func shouldTrigger(e WatchEvent) bool {
	if e.Reason != "" {
		return true
	}

	// Generate release reports for successful releases as well.
	// This makes PASS releases first-class records for future Release Memory.
	return strings.EqualFold(e.RolloutPhase, "Healthy") &&
		strings.EqualFold(e.AnalysisRunPhase, "Successful")
}

func runScript(repoDir string, env []string, script string) error {
	cmd := exec.Command("bash", script)
	cmd.Dir = repoDir
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runReportJob(cfg Config, e WatchEvent) {
	incWatcherTriggeredReports()

	log.Printf(
		"trigger report job: namespace=%s rollout=%s phase=%s abort=%v analysisrun=%s analysisrunPhase=%s reason=%s",
		e.Namespace,
		e.RolloutName,
		e.RolloutPhase,
		e.RolloutAbort,
		e.AnalysisRunName,
		e.AnalysisRunPhase,
		e.Reason,
	)

	env := os.Environ()
	env = append(env, "OLLAMA_URL="+cfg.OllamaURL)
	env = append(env, "MODEL="+cfg.Model)

	contextFile, err := writeReleaseContext(cfg, e)
	if err != nil {
		log.Printf("failed to write release context: %v", err)
	} else {
		log.Printf("release context generated: %s", contextFile)
		env = append(env, "RELEASE_CONTEXT_FILE="+contextFile)
	}

	env = append(env, "RELEASE_NAMESPACE="+e.Namespace)
	env = append(env, "RELEASE_ROLLOUT="+e.RolloutName)

	if err := runScript(cfg.RepoDir, env, "scripts/collect-release-report.sh"); err != nil {
		log.Printf("collect-release-report.sh failed: %v", err)
	}

	if err := runScript(cfg.RepoDir, env, "scripts/ai-release-advisor.sh"); err != nil {
		log.Printf("ai-release-advisor.sh failed: %v", err)
	}

	log.Printf("report job finished")
}

func processTarget(ctx context.Context, client dynamic.Interface, cfg Config, target Target) {
	incWatcherChecks()

	rollout, err := client.Resource(rolloutGVR).Namespace(target.Namespace).Get(ctx, target.Rollout, metav1.GetOptions{})
	if err != nil {
		incWatcherErrors()
		log.Printf("failed to get rollout %s/%s: %v", target.Namespace, target.Rollout, err)
		return
	}

	watcherReady.Store(true)

	ar, err := latestAnalysisRun(ctx, client, target)
	if err != nil {
		log.Printf("failed to list analysisruns for %s/%s: %v", target.Namespace, target.Rollout, err)
	}

	event := buildEvent(target, rollout, ar)

	log.Printf(
		"check: namespace=%s rollout=%s rolloutPhase=%s abort=%v analysisRun=%s analysisRunPhase=%s",
		event.Namespace,
		event.RolloutName,
		event.RolloutPhase,
		event.RolloutAbort,
		event.AnalysisRunName,
		event.AnalysisRunPhase,
	)

	if !shouldTrigger(event) {
		return
	}

	state := loadState(cfg.StateFile)

	if contains(state.Processed, event.Key) {
		log.Printf("event already processed: %s", event.Key)
		return
	}

	runReportJob(cfg, event)

	state.Processed = append(state.Processed, event.Key)
	state.Processed = tailKeep(state.Processed, 100)
	saveState(cfg.StateFile, state)
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func notifyWatchEvent(ctx context.Context, ch chan<- string, reason string) {
	select {
	case ch <- reason:
	case <-ctx.Done():
	default:
		log.Printf("watch event skipped because trigger queue is full: reason=%s", reason)
	}
}

func watchRolloutLoop(ctx context.Context, client dynamic.Interface, target Target, triggerCh chan<- string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		list, err := client.Resource(rolloutGVR).Namespace(target.Namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			incWatcherErrors()
			incWatcherWatchRestarts()
			log.Printf("list rollout before watch failed: namespace=%s rollout=%s error=%v", target.Namespace, target.Rollout, err)
			if !sleepWithContext(ctx, 5*time.Second) {
				return
			}
			continue
		}

		resourceVersion := list.GetResourceVersion()

		w, err := client.Resource(rolloutGVR).Namespace(target.Namespace).Watch(ctx, metav1.ListOptions{
			ResourceVersion: resourceVersion,
		})
		if err != nil {
			incWatcherErrors()
			incWatcherWatchRestarts()
			log.Printf("watch rollout failed: namespace=%s rollout=%s resourceVersion=%s error=%v", target.Namespace, target.Rollout, resourceVersion, err)
			if !sleepWithContext(ctx, 5*time.Second) {
				return
			}
			continue
		}

		log.Printf("watch started: resource=rollout namespace=%s rollout=%s resourceVersion=%s", target.Namespace, target.Rollout, resourceVersion)

		for event := range w.ResultChan() {
			incWatcherWatchEvents()

			if event.Type == watchpkg.Error {
				incWatcherErrors()
				log.Printf("watch rollout error event: namespace=%s rollout=%s", target.Namespace, target.Rollout)
				continue
			}

			obj, ok := event.Object.(*unstructured.Unstructured)
			if !ok || obj == nil {
				log.Printf("watch rollout skipped non-unstructured event: type=%s", event.Type)
				continue
			}

			if obj.GetName() != target.Rollout {
				continue
			}

			log.Printf("watch event: resource=rollout type=%s namespace=%s name=%s", event.Type, target.Namespace, obj.GetName())
			notifyWatchEvent(ctx, triggerCh, fmt.Sprintf("rollout/%s/%s/%s", target.Namespace, target.Rollout, event.Type))
		}

		incWatcherWatchRestarts()
		log.Printf("watch rollout channel closed: namespace=%s rollout=%s; restarting watch", target.Namespace, target.Rollout)

		if !sleepWithContext(ctx, 2*time.Second) {
			return
		}
	}
}

func watchAnalysisRunLoop(ctx context.Context, client dynamic.Interface, target Target, triggerCh chan<- string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		list, err := client.Resource(analysisRunGVR).Namespace(target.Namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			incWatcherErrors()
			incWatcherWatchRestarts()
			log.Printf("list analysisrun before watch failed: namespace=%s rollout=%s error=%v", target.Namespace, target.Rollout, err)
			if !sleepWithContext(ctx, 5*time.Second) {
				return
			}
			continue
		}

		resourceVersion := list.GetResourceVersion()

		w, err := client.Resource(analysisRunGVR).Namespace(target.Namespace).Watch(ctx, metav1.ListOptions{
			ResourceVersion: resourceVersion,
		})
		if err != nil {
			incWatcherErrors()
			incWatcherWatchRestarts()
			log.Printf("watch analysisrun failed: namespace=%s rollout=%s resourceVersion=%s error=%v", target.Namespace, target.Rollout, resourceVersion, err)
			if !sleepWithContext(ctx, 5*time.Second) {
				return
			}
			continue
		}

		log.Printf("watch started: resource=analysisrun namespace=%s rollout=%s resourceVersion=%s", target.Namespace, target.Rollout, resourceVersion)

		for event := range w.ResultChan() {
			incWatcherWatchEvents()

			if event.Type == watchpkg.Error {
				incWatcherErrors()
				log.Printf("watch analysisrun error event: namespace=%s rollout=%s", target.Namespace, target.Rollout)
				continue
			}

			obj, ok := event.Object.(*unstructured.Unstructured)
			if !ok || obj == nil {
				log.Printf("watch analysisrun skipped non-unstructured event: type=%s", event.Type)
				continue
			}

			name := obj.GetName()
			if !strings.HasPrefix(name, target.Rollout+"-") {
				continue
			}

			log.Printf("watch event: resource=analysisrun type=%s namespace=%s name=%s", event.Type, target.Namespace, name)
			notifyWatchEvent(ctx, triggerCh, fmt.Sprintf("analysisrun/%s/%s/%s", target.Namespace, name, event.Type))
		}

		incWatcherWatchRestarts()
		log.Printf("watch analysisrun channel closed: namespace=%s rollout=%s; restarting watch", target.Namespace, target.Rollout)

		if !sleepWithContext(ctx, 2*time.Second) {
			return
		}
	}
}

func runWatchLoop(ctx context.Context, client dynamic.Interface, cfg Config, interval time.Duration) {
	resyncInterval := interval * 6
	if resyncInterval < 30*time.Second {
		resyncInterval = 30 * time.Second
	}

	log.Printf("watch loop started: targets=%d resyncInterval=%s", len(cfg.Targets), resyncInterval.String())

	triggerCh := make(chan string, 32)

	for _, target := range cfg.Targets {
		go watchRolloutLoop(ctx, client, target, triggerCh)
		go watchAnalysisRunLoop(ctx, client, target, triggerCh)
	}

	log.Printf("watch initial sync started")
	for _, target := range cfg.Targets {
		processTarget(ctx, client, cfg, target)
	}

	ticker := time.NewTicker(resyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case reason := <-triggerCh:
			reasons := []string{reason}

			debounceTimer := time.NewTimer(500 * time.Millisecond)
			collecting := true

			for collecting {
				select {
				case extraReason := <-triggerCh:
					reasons = append(reasons, extraReason)
				case <-debounceTimer.C:
					collecting = false
				case <-ctx.Done():
					debounceTimer.Stop()
					return
				}
			}

			log.Printf("watch trigger batch received: count=%d first=%s", len(reasons), reasons[0])

			for _, target := range cfg.Targets {
				processTarget(ctx, client, cfg, target)
			}
		case <-ticker.C:
			log.Printf("watch fallback resync started")
			for _, target := range cfg.Targets {
				processTarget(ctx, client, cfg, target)
			}
		}
	}
}

func main() {
	configPath := flag.String("config", "", "config yaml path")

	namespaceOverride := flag.String("namespace", "", "override single rollout namespace")
	rolloutOverride := flag.String("rollout", "", "override single rollout name")
	intervalOverride := flag.String("interval", "", "override fallback resync interval, example: 10s")
	repoDirOverride := flag.String("repo-dir", "", "override repository directory")
	stateFileOverride := flag.String("state-file", "", "override state file")
	ollamaURLOverride := flag.String("ollama-url", "", "override ollama api url")
	modelOverride := flag.String("model", "", "override ollama model")
	healthAddrOverride := flag.String("health-addr", "", "override health server address, example: :8080")

	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// 命令行覆盖：如果传了 namespace + rollout，则只监听这个单 target。
	if *namespaceOverride != "" && *rolloutOverride != "" {
		cfg.Targets = []Target{
			{
				Namespace: *namespaceOverride,
				Rollout:   *rolloutOverride,
			},
		}
	}

	if *intervalOverride != "" {
		cfg.Interval = *intervalOverride
	}
	if *repoDirOverride != "" {
		cfg.RepoDir = *repoDirOverride
	}
	if *stateFileOverride != "" {
		cfg.StateFile = *stateFileOverride
	}
	if *ollamaURLOverride != "" {
		cfg.OllamaURL = *ollamaURLOverride
	}
	if *modelOverride != "" {
		cfg.Model = *modelOverride
	}
	if *healthAddrOverride != "" {
		cfg.HealthAddr = *healthAddrOverride
	}

	interval, err := time.ParseDuration(cfg.Interval)
	if err != nil {
		log.Fatalf("invalid interval %q: %v", cfg.Interval, err)
	}

	kubeCfg, err := buildKubeConfig()
	if err != nil {
		log.Fatalf("failed to build kube config: %v", err)
	}

	client, err := dynamic.NewForConfig(kubeCfg)
	if err != nil {
		log.Fatalf("failed to create dynamic client: %v", err)
	}

	log.Printf(
		"go rollout watcher started: targets=%d mode=watch-only interval=%s repoDir=%s stateFile=%s model=%s ollamaURL=%s healthAddr=%s",
		len(cfg.Targets),
		interval.String(),
		cfg.RepoDir,
		cfg.StateFile,
		cfg.Model,
		cfg.OllamaURL,
		cfg.HealthAddr,
	)

	for _, target := range cfg.Targets {
		log.Printf("watch target: namespace=%s rollout=%s", target.Namespace, target.Rollout)
	}

	ctx := context.Background()

	go startHealthServer(cfg.HealthAddr, cfg)

	log.Printf("watcher is running in watch-only mode")
	runWatchLoop(ctx, client, cfg, interval)
}
