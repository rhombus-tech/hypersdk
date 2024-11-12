// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package offchain

import (
    "context"
    "errors"
    "fmt"
    "runtime"
    "sync"
    "time"

    "github.com/ava-labs/avalanchego/utils/logging"
    "github.com/shirou/gopsutil/cpu"
    "github.com/shirou/gopsutil/mem"
    "github.com/shirou/gopsutil/process"
)

var (
    ErrMonitorStopped     = errors.New("monitor stopped")
    ErrResourceExhausted  = errors.New("resource limit exceeded")
    ErrUnhealthyWorker    = errors.New("worker unhealthy")
)

// ResourceMonitor tracks system and worker resource usage
type ResourceMonitor struct {
    manager     *Manager
    log         logging.Logger
    config      *MonitorConfig

    // Resource tracking
    workerStats map[uint64]*WorkerStats
    systemStats *SystemStats
    healthState *HealthState
    
    // Alert channels
    alerts      chan Alert
    
    // Metrics
    metrics     *MonitorMetrics

    // Synchronization
    lock        sync.RWMutex
    ctx         context.Context
    cancel      context.CancelFunc
    wg          sync.WaitGroup
}

type MonitorConfig struct {
    // Resource limits
    MaxCPUPercent    float64
    MaxMemoryPercent float64
    MaxGoroutines    int
    
    // Monitoring intervals
    CheckInterval    time.Duration
    StatsInterval   time.Duration
    
    // Health check
    HealthCheckInterval time.Duration
    UnhealthyThreshold  int
    
    // Alerts
    AlertQueueSize     int
    AlertThrottleTime  time.Duration
}

type WorkerStats struct {
    CPU       float64
    Memory    uint64
    Goroutines int
    TaskCount  uint64
    LastActive time.Time
    Errors     int
}

type SystemStats struct {
    CPUUsage     float64
    MemoryUsage  uint64
    TotalMemory  uint64
    GoroutineCount int
    Time          time.Time
}

type HealthState struct {
    Healthy         bool
    UnhealthyCount  int
    LastCheck       time.Time
    FailureReason   string
}

type Alert struct {
    Type      AlertType
    Message   string
    Resource  string
    Value     float64
    Threshold float64
    Time      time.Time
}

type AlertType int

const (
    AlertCPU AlertType = iota
    AlertMemory
    AlertGoroutines
    AlertWorkerError
    AlertSystemError
)

type MonitorMetrics struct {
    ChecksPerformed  uint64
    AlertsGenerated  uint64
    HealthChecks     uint64
    UnhealthyPeriods uint64
    PeakCPU         float64
    PeakMemory      uint64
    lock            sync.Mutex
}

// NewResourceMonitor creates a new resource monitor
func NewResourceMonitor(
    manager *Manager,
    config *MonitorConfig,
    log logging.Logger,
) *ResourceMonitor {
    ctx, cancel := context.WithCancel(context.Background())

    return &ResourceMonitor{
        manager:     manager,
        config:      config,
        log:         log,
        workerStats: make(map[uint64]*WorkerStats),
        systemStats: &SystemStats{},
        healthState: &HealthState{Healthy: true},
        alerts:     make(chan Alert, config.AlertQueueSize),
        metrics:    &MonitorMetrics{},
        ctx:        ctx,
        cancel:     cancel,
    }
}

// Start begins resource monitoring
func (m *ResourceMonitor) Start() error {
    // Start resource monitoring
    m.wg.Add(1)
    go m.monitorResources()

    // Start health checks
    m.wg.Add(1)
    go m.performHealthChecks()

    // Start stats collection
    m.wg.Add(1)
    go m.collectStats()

    // Start alert processor
    m.wg.Add(1)
    go m.processAlerts()

    return nil
}

// Stop gracefully shuts down the monitor
func (m *ResourceMonitor) Stop() error {
    m.cancel()
    m.wg.Wait()
    return nil
}

func (m *ResourceMonitor) monitorResources() {
    defer m.wg.Done()

    ticker := time.NewTicker(m.config.CheckInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            m.checkResources()
        case <-m.ctx.Done():
            return
        }
    }
}

func (m *ResourceMonitor) checkResources() {
    // Update system stats
    if err := m.updateSystemStats(); err != nil {
        m.log.Error("Failed to update system stats: %v", err)
        return
    }

    // Check CPU usage
    if m.systemStats.CPUUsage > m.config.MaxCPUPercent {
        m.generateAlert(AlertCPU, "CPU usage exceeded threshold",
            "cpu", m.systemStats.CPUUsage, m.config.MaxCPUPercent)
    }

    // Check memory usage
    memoryPercent := float64(m.systemStats.MemoryUsage) / float64(m.systemStats.TotalMemory) * 100
    if memoryPercent > m.config.MaxMemoryPercent {
        m.generateAlert(AlertMemory, "Memory usage exceeded threshold",
            "memory", memoryPercent, m.config.MaxMemoryPercent)
    }

    // Check goroutine count
    if m.systemStats.GoroutineCount > m.config.MaxGoroutines {
        m.generateAlert(AlertGoroutines, "Goroutine count exceeded threshold",
            "goroutines", float64(m.systemStats.GoroutineCount), float64(m.config.MaxGoroutines))
    }

    // Update metrics
    m.updateMetrics()
}

func (m *ResourceMonitor) updateSystemStats() error {
    // Get CPU usage
    cpuPercent, err := cpu.Percent(0, false)
    if err != nil {
        return fmt.Errorf("failed to get CPU usage: %w", err)
    }
    if len(cpuPercent) > 0 {
        m.systemStats.CPUUsage = cpuPercent[0]
    }

    // Get memory usage
    memInfo, err := mem.VirtualMemory()
    if err != nil {
        return fmt.Errorf("failed to get memory info: %w", err)
    }
    m.systemStats.MemoryUsage = memInfo.Used
    m.systemStats.TotalMemory = memInfo.Total

    // Get goroutine count
    m.systemStats.GoroutineCount = runtime.NumGoroutine()
    m.systemStats.Time = time.Now()

    return nil
}

func (m *ResourceMonitor) performHealthChecks() {
    defer m.wg.Done()

    ticker := time.NewTicker(m.config.HealthCheckInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            m.checkHealth()
        case <-m.ctx.Done():
            return
        }
    }
}

func (m *ResourceMonitor) checkHealth() {
    m.lock.Lock()
    defer m.lock.Unlock()

    // Check worker health
    unhealthyWorkers := 0
    for id, stats := range m.workerStats {
        if time.Since(stats.LastActive) > m.config.HealthCheckInterval*2 {
            unhealthyWorkers++
            m.log.Warn("Worker %d appears inactive", id)
        }
        
        if stats.Errors > m.config.UnhealthyThreshold {
            unhealthyWorkers++
            m.log.Warn("Worker %d exceeded error threshold", id)
        }
    }

    // Update health state
    m.healthState.LastCheck = time.Now()
    if unhealthyWorkers > 0 {
        m.healthState.UnhealthyCount++
        if m.healthState.UnhealthyCount >= m.config.UnhealthyThreshold {
            m.healthState.Healthy = false
            m.healthState.FailureReason = fmt.Sprintf("%d unhealthy workers", unhealthyWorkers)
        }
    } else {
        m.healthState.UnhealthyCount = 0
        m.healthState.Healthy = true
        m.healthState.FailureReason = ""
    }

    // Update metrics
    m.metrics.lock.Lock()
    m.metrics.HealthChecks++
    if !m.healthState.Healthy {
        m.metrics.UnhealthyPeriods++
    }
    m.metrics.lock.Unlock()
}

func (m *ResourceMonitor) collectStats() {
    defer m.wg.Done()

    ticker := time.NewTicker(m.config.StatsInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            m.updateWorkerStats()
        case <-m.ctx.Done():
            return
        }
    }
}

func (m *ResourceMonitor) updateWorkerStats() {
    for _, worker := range m.manager.workers {
        stats := &WorkerStats{
            TaskCount:  worker.GetTaskCount(),
            LastActive: worker.GetLastActiveTime(),
            Errors:    worker.GetErrorCount(),
        }

        // Get process stats if available
        if proc, err := process.NewProcess(int32(worker.GetPID())); err == nil {
            if cpu, err := proc.CPUPercent(); err == nil {
                stats.CPU = cpu
            }
            if mem, err := proc.MemoryInfo(); err == nil {
                stats.Memory = mem.RSS
            }
        }

        m.lock.Lock()
        m.workerStats[worker.GetID()] = stats
        m.lock.Unlock()
    }
}

func (m *ResourceMonitor) processAlerts() {
    defer m.wg.Done()

    throttle := make(map[AlertType]time.Time)

    for {
        select {
        case alert := <-m.alerts:
            // Check throttling
            if lastAlert, ok := throttle[alert.Type]; ok {
                if time.Since(lastAlert) < m.config.AlertThrottleTime {
                    continue
                }
            }
            throttle[alert.Type] = time.Now()

            // Log alert
            m.log.Warn("Resource alert: %s - %s (current: %.2f, threshold: %.2f)",
                alert.Type, alert.Message, alert.Value, alert.Threshold)

            // Update metrics
            m.metrics.lock.Lock()
            m.metrics.AlertsGenerated++
            m.metrics.lock.Unlock()

        case <-m.ctx.Done():
            return
        }
    }
}

func (m *ResourceMonitor) generateAlert(
    alertType AlertType,
    message string,
    resource string,
    value float64,
    threshold float64,
) {
    alert := Alert{
        Type:      alertType,
        Message:   message,
        Resource:  resource,
        Value:     value,
        Threshold: threshold,
        Time:      time.Now(),
    }

    select {
    case m.alerts <- alert:
    default:
        m.log.Warn("Alert queue full, dropping alert")
    }
}

func (m *ResourceMonitor) updateMetrics() {
    m.metrics.lock.Lock()
    defer m.metrics.lock.Unlock()

    m.metrics.ChecksPerformed++
    if m.systemStats.CPUUsage > m.metrics.PeakCPU {
        m.metrics.PeakCPU = m.systemStats.CPUUsage
    }
    if m.systemStats.MemoryUsage > m.metrics.PeakMemory {
        m.metrics.PeakMemory = m.systemStats.MemoryUsage
    }
}

// GetHealthStatus returns current health state
func (m *ResourceMonitor) GetHealthStatus() *HealthState {
    m.lock.RLock()
    defer m.lock.RUnlock()
    return m.healthState
}

// GetMetrics returns current monitor metrics
func (m *ResourceMonitor) GetMetrics() MonitorMetrics {
    m.metrics.lock.Lock()
    defer m.metrics.lock.Unlock()
    return *m.metrics
}

// GetWorkerStats returns stats for all workers
func (m *ResourceMonitor) GetWorkerStats() map[uint64]*WorkerStats {
    m.lock.RLock()
    defer m.lock.RUnlock()
    
    // Return copy to prevent external modification
    stats := make(map[uint64]*WorkerStats)
    for id, workerStats := range m.workerStats {
        statsCopy := *workerStats
        stats[id] = &statsCopy
    }
    return stats
}

// GetSystemStats returns current system stats
func (m *ResourceMonitor) GetSystemStats() SystemStats {
    m.lock.RLock()
    defer m.lock.RUnlock()
    return *m.systemStats
}