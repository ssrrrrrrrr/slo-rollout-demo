package main

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"
)

const DefaultReliabilityReconcileInterval = 30 * time.Second
const DefaultReliabilityReconcileConcurrency = 4
const DefaultReliabilityReconcileTimeout = 15 * time.Second

type ReliabilityControllerStatus struct {
	Enabled             bool   `json:"enabled"`
	Running             bool   `json:"running"`
	Interval            string `json:"interval"`
	Concurrency         int    `json:"concurrency"`
	LastCycleStartedAt  string `json:"lastCycleStartedAt,omitempty"`
	LastCycleFinishedAt string `json:"lastCycleFinishedAt,omitempty"`
	LastCycleDurationMs int64  `json:"lastCycleDurationMs,omitempty"`
	ServicesEvaluated   int    `json:"servicesEvaluated"`
	ServicesSucceeded   int    `json:"servicesSucceeded"`
	ServicesFailed      int    `json:"servicesFailed"`
	NextRunApprox       string `json:"nextRunApprox,omitempty"`
}

// ReliabilityController schedules only durable lifecycle reconciliation. It
// deliberately has no dependency on providers, detector, repository writes,
// Agent, Recovery, or Operation execution.
type ReliabilityController struct {
	services    *ServiceService
	lifecycle   *IncidentLifecycleService
	interval    time.Duration
	concurrency int
	timeout     time.Duration
	reconcile   func(context.Context, *http.Request, string) (*ReliabilityIncident, error)
	cycleMu     sync.Mutex
	statusMu    sync.RWMutex
	status      ReliabilityControllerStatus
}

func NewReliabilityController(services *ServiceService, lifecycle *IncidentLifecycleService, enabled bool, interval time.Duration, concurrency int) *ReliabilityController {
	if interval <= 0 {
		interval = DefaultReliabilityReconcileInterval
	}
	if concurrency <= 0 {
		concurrency = DefaultReliabilityReconcileConcurrency
	}
	controller := &ReliabilityController{services: services, lifecycle: lifecycle, interval: interval, concurrency: concurrency, timeout: DefaultReliabilityReconcileTimeout, status: ReliabilityControllerStatus{Enabled: enabled, Interval: interval.String(), Concurrency: concurrency}}
	controller.reconcile = lifecycle.ReconcileService
	return controller
}

func (c *ReliabilityController) Run(ctx context.Context) {
	if !c.status.Enabled || c.lifecycle == nil {
		return
	}
	c.setRunning(true)
	defer c.setRunning(false)
	c.ReconcileOnce(ctx)
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		c.setNext(time.Now().Add(c.interval))
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.ReconcileOnce(ctx)
		}
	}
}

// ReconcileOnce serializes whole cycles and bounds only per-service work.
func (c *ReliabilityController) ReconcileOnce(ctx context.Context) {
	if !c.status.Enabled || c.lifecycle == nil {
		return
	}
	c.cycleMu.Lock()
	defer c.cycleMu.Unlock()
	started := time.Now().UTC()
	c.setCycleStart(started)
	services, err := c.services.Load()
	if err != nil {
		log.Printf("reliability controller cycle failed to load services: %v", err)
		c.setCycleFinish(started, 0, 0, 1)
		return
	}
	type result struct{ err error }
	jobs := make(chan Service)
	results := make(chan result, len(services))
	workers := c.concurrency
	if workers > len(services) {
		workers = len(services)
	}
	var wait sync.WaitGroup
	for i := 0; i < workers; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for service := range jobs {
				serviceCtx, cancel := context.WithTimeout(ctx, c.timeout)
				request, _ := http.NewRequestWithContext(serviceCtx, http.MethodGet, "http://reliability-controller.local/", nil)
				_, err := c.reconcile(serviceCtx, request, service.Metadata.Name)
				cancel()
				if err != nil {
					log.Printf("reliability controller reconcile failed: service=%s error=%v", service.Metadata.Name, err)
				}
				results <- result{err: err}
			}
		}()
	}
dispatch:
	for _, service := range services {
		select {
		case jobs <- service:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	wait.Wait()
	close(results)
	succeeded, failed := 0, 0
	for result := range results {
		if result.err != nil {
			failed++
		} else {
			succeeded++
		}
	}
	c.setCycleFinish(started, len(services), succeeded, failed)
	log.Printf("reliability controller cycle finished: services=%d succeeded=%d failed=%d duration=%s", len(services), succeeded, failed, time.Since(started).Round(time.Millisecond))
}

func (c *ReliabilityController) Status() ReliabilityControllerStatus {
	c.statusMu.RLock()
	defer c.statusMu.RUnlock()
	return c.status
}
func (c *ReliabilityController) setRunning(value bool) {
	c.statusMu.Lock()
	c.status.Running = value
	c.statusMu.Unlock()
}
func (c *ReliabilityController) setNext(value time.Time) {
	c.statusMu.Lock()
	c.status.NextRunApprox = value.UTC().Format(time.RFC3339)
	c.statusMu.Unlock()
}
func (c *ReliabilityController) setCycleStart(value time.Time) {
	c.statusMu.Lock()
	c.status.LastCycleStartedAt = value.Format(time.RFC3339)
	c.status.ServicesEvaluated = 0
	c.status.ServicesSucceeded = 0
	c.status.ServicesFailed = 0
	c.statusMu.Unlock()
	log.Printf("reliability controller cycle started")
}
func (c *ReliabilityController) setCycleFinish(started time.Time, evaluated, succeeded, failed int) {
	finished := time.Now().UTC()
	c.statusMu.Lock()
	c.status.LastCycleFinishedAt = finished.Format(time.RFC3339)
	c.status.LastCycleDurationMs = finished.Sub(started).Milliseconds()
	c.status.ServicesEvaluated = evaluated
	c.status.ServicesSucceeded = succeeded
	c.status.ServicesFailed = failed
	c.statusMu.Unlock()
}
