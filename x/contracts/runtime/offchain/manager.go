// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package offchain

import (
    "context"
    "errors"
    "fmt"
    "sync"
    "time"

    "github.com/ava-labs/avalanchego/ids"
    "github.com/ava-labs/avalanchego/utils/logging"
    "github.com/ava-labs/hypersdk/chain"
    "github.com/ava-labs/hypersdk/codec"
    "github.com/ava-labs/hypersdk/events"
    "github.com/ava-labs/hypersdk/runtime"
)

var (
    ErrManagerStopped     = errors.New("manager stopped")
    ErrInvalidConfig      = errors.New("invalid configuration")
    ErrWorkerUnavailable  = errors.New("no workers available")
)

// Manager coordinates off-chain worker components
type Manager struct {
    // Core components
    runtime      *runtime.WasmRuntime
    executor     *Executor
    storage      *LocalStorage
    eventManager *events.OffChainEventManager
    txSubmitter  *TransactionSubmitter
    log         logging.Logger

    // Configuration
    config      *ManagerConfig
    workers     []*Worker
    workerPool  chan *Worker

    // State management
    chainState  runtime.StateManager
    currentBlock uint64
    blockHash    ids.ID
    lock         sync.RWMutex

    // Task management
    taskQueue    chan Task
    results      chan Result

    // Metrics
    metrics      *ManagerMetrics

    // Lifecycle management
    ctx          context.Context
    cancel       context.CancelFunc
    wg           sync.WaitGroup
}

type ManagerConfig struct {
    // Worker configuration
    WorkerCount     int
    WorkerConfig    *WorkerConfig
    
    // Storage configuration
    StorageConfig   *StorageConfig
    
    // Executor configuration
    ExecutorConfig  *ExecutorConfig
    
    // Event configuration
    EventConfig     *events.OffChainConfig
    
    // Transaction configuration
    TxConfig        *TxSubmitterConfig
    
    // Task management
    TaskQueueSize   int
    ResultQueueSize int
    MaxRetries      int
    RetryDelay      time.Duration
}

type ManagerMetrics struct {
    TasksScheduled   uint64
    TasksCompleted   uint64
    TasksFailed      uint64
    TransactionsSent uint64
    EventsEmitted    uint64
    AverageLatency   time.Duration
    lock             sync.Mutex
}

// NewManager creates a new off-chain manager instance
func NewManager(
    runtime *runtime.WasmRuntime,
    chainState runtime.StateManager,
    config *ManagerConfig,
    log logging.Logger,
) (*Manager, error) {
    if err := validateConfig(config); err != nil {
        return nil, err
    }

    ctx, cancel := context.WithCancel(context.Background())

    // Initialize storage
    storage, err := NewLocalStorage(config.StorageConfig, log)
    if err != nil {
        cancel()
        return nil, fmt.Errorf("failed to initialize storage: %w", err)
    }

    // Initialize event manager
    eventManager := events.NewOffChainEventManager(
        runtime.Events(),
        config.EventConfig,
        NewEventStorage(storage),
        log,
    )

    // Initialize transaction submitter
    txSubmitter := NewTransactionSubmitter(
        runtime.GetTxPool(),
        runtime.GetSigner(),
        chainState,
        config.TxConfig,
        log,
    )

    manager := &Manager{
        runtime:      runtime,
        chainState:   chainState,
        storage:      storage,
        eventManager: eventManager,
        txSubmitter:  txSubmitter,
        log:         log,
        config:      config,
        workerPool:  make(chan *Worker, config.WorkerCount),
        taskQueue:   make(chan Task, config.TaskQueueSize),
        results:     make(chan Result, config.ResultQueueSize),
        metrics:     &ManagerMetrics{},
        ctx:         ctx,
        cancel:      cancel,
    }

    // Initialize executor
    executor, err := NewExecutor(runtime, manager, config.ExecutorConfig, log)
    if err != nil {
        cancel()
        return nil, fmt.Errorf("failed to initialize executor: %w", err)
    }
    manager.executor = executor

    // Initialize workers
    if err := manager.initializeWorkers(); err != nil {
        cancel()
        return nil, fmt.Errorf("failed to initialize workers: %w", err)
    }

    return manager, nil
}

// Start begins off-chain processing
func (m *Manager) Start() error {
    m.log.Info("Starting off-chain manager")

    // Start components
    if err := m.storage.Start(); err != nil {
        return fmt.Errorf("failed to start storage: %w", err)
    }

    if err := m.eventManager.Start(); err != nil {
        return fmt.Errorf("failed to start event manager: %w", err)
    }

    if err := m.txSubmitter.Start(); err != nil {
        return fmt.Errorf("failed to start transaction submitter: %w", err)
    }

    if err := m.executor.Start(); err != nil {
        return fmt.Errorf("failed to start executor: %w", err)
    }

    // Start worker pool
    for _, worker := range m.workers {
        if err := worker.Start(); err != nil {
            return fmt.Errorf("failed to start worker: %w", err)
        }
        m.workerPool <- worker
    }

    // Start task processor
    m.wg.Add(1)
    go m.processTaskQueue()

    // Start result processor
    m.wg.Add(1)
    go m.processResults()

    return nil
}

// Stop gracefully shuts down the manager
func (m *Manager) Stop() error {
    m.log.Info("Stopping off-chain manager")
    m.cancel()
    m.wg.Wait()

    // Stop components in reverse order
    if err := m.executor.Stop(); err != nil {
        m.log.Error("Failed to stop executor: %v", err)
    }

    if err := m.txSubmitter.Stop(); err != nil {
        m.log.Error("Failed to stop transaction submitter: %v", err)
    }

    if err := m.eventManager.Stop(); err != nil {
        m.log.Error("Failed to stop event manager: %v", err)
    }

    if err := m.storage.Stop(); err != nil {
        m.log.Error("Failed to stop storage: %v", err)
    }

    for _, worker := range m.workers {
        if err := worker.Stop(); err != nil {
            m.log.Error("Failed to stop worker: %v", err)
        }
    }

    return nil
}

// ScheduleTask submits a task for execution
func (m *Manager) ScheduleTask(task Task) error {
    if err := m.validateTask(task); err != nil {
        return err
    }

    select {
    case m.taskQueue <- task:
        m.updateMetrics(true, 0, 0)
        return nil
    case <-m.ctx.Done():
        return ErrManagerStopped
    default:
        return fmt.Errorf("task queue full")
    }
}

// UpdateBlock updates the current block context
func (m *Manager) UpdateBlock(height uint64, hash ids.ID) {
    m.lock.Lock()
    defer m.lock.Unlock()

    m.currentBlock = height
    m.blockHash = hash

    // Update components
    m.executor.UpdateBlock(height, hash)
    for _, worker := range m.workers {
        worker.UpdateBlock(height, hash)
    }
}

// Internal methods

func (m *Manager) initializeWorkers() error {
    m.workers = make([]*Worker, m.config.WorkerCount)
    for i := 0; i < m.config.WorkerCount; i++ {
        worker, err := NewWorker(m, m.config.WorkerConfig, m.log)
        if err != nil {
            return fmt.Errorf("failed to create worker %d: %w", i, err)
        }
        m.workers[i] = worker
    }
    return nil
}

func (m *Manager) processTaskQueue() {
    defer m.wg.Done()

    for {
        select {
        case task := <-m.taskQueue:
            if err := m.assignTask(task); err != nil {
                m.log.Error("Failed to assign task: %v", err)
            }
        case <-m.ctx.Done():
            return
        }
    }
}

func (m *Manager) assignTask(task Task) error {
    select {
    case worker := <-m.workerPool:
        go func() {
            defer func() {
                m.workerPool <- worker
            }()

            result := worker.ExecuteTask(task)
            select {
            case m.results <- result:
            case <-m.ctx.Done():
            }
        }()
        return nil
    case <-m.ctx.Done():
        return ErrManagerStopped
    default:
        return ErrWorkerUnavailable
    }
}

func (m *Manager) processResults() {
    defer m.wg.Done()

    for {
        select {
        case result := <-m.results:
            m.handleResult(result)
        case <-m.ctx.Done():
            return
        }
    }
}

func (m *Manager) handleResult(result Result) {
    start := time.Now()

    // Process events
    for _, event := range result.Events {
        if err := m.eventManager.Emit(event); err != nil {
            m.log.Error("Failed to emit event: %v", err)
        }
    }

    // Submit transactions
    for _, tx := range result.GeneratedTxs {
        if err := m.txSubmitter.Submit(tx); err != nil {
            m.log.Error("Failed to submit transaction: %v", err)
        }
    }

    // Update metrics
    m.updateMetrics(false, len(result.GeneratedTxs), len(result.Events))
    m.updateLatency(time.Since(start))
}

func (m *Manager) validateTask(task Task) error {
    if task.Contract == codec.EmptyAddress {
        return errors.New("invalid contract address")
    }

    if task.Function == "" {
        return errors.New("empty function name")
    }

    return nil
}

// Metrics management

func (m *Manager) updateMetrics(scheduled bool, txCount, eventCount int) {
    m.metrics.lock.Lock()
    defer m.metrics.lock.Unlock()

    if scheduled {
        m.metrics.TasksScheduled++
    } else {
        m.metrics.TasksCompleted++
    }

    m.metrics.TransactionsSent += uint64(txCount)
    m.metrics.EventsEmitted += uint64(eventCount)
}

func (m *Manager) updateLatency(duration time.Duration) {
    m.metrics.lock.Lock()
    defer m.metrics.lock.Unlock()

    // Simple moving average
    if m.metrics.TasksCompleted > 0 {
        m.metrics.AverageLatency = (m.metrics.AverageLatency + duration) / 2
    } else {
        m.metrics.AverageLatency = duration
    }
}

// GetMetrics returns current manager metrics
func (m *Manager) GetMetrics() ManagerMetrics {
    m.metrics.lock.Lock()
    defer m.metrics.lock.Unlock()
    return *m.metrics
}

// Configuration validation
func validateConfig(config *ManagerConfig) error {
    if config.WorkerCount <= 0 {
        return fmt.Errorf("%w: invalid worker count", ErrInvalidConfig)
    }

    if config.TaskQueueSize <= 0 {
        return fmt.Errorf("%w: invalid task queue size", ErrInvalidConfig)
    }

    if config.ResultQueueSize <= 0 {
        return fmt.Errorf("%w: invalid result queue size", ErrInvalidConfig)
    }

    if config.MaxRetries < 0 {
        return fmt.Errorf("%w: invalid max retries", ErrInvalidConfig)
    }

    return nil
}