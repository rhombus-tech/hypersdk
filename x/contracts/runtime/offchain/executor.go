// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package offchain

import (
    "context"
    "errors"
    "fmt"
    "sync"
    "time"

    "github.com/ava-labs/avalanchego/utils/logging"
    "github.com/ava-labs/hypersdk/chain"
    "github.com/ava-labs/hypersdk/codec"
    "github.com/ava-labs/hypersdk/runtime"
)

var (
    ErrExecutorStopped    = errors.New("executor stopped")
    ErrExecutorBusy       = errors.New("executor busy")
    ErrInvalidTask        = errors.New("invalid task")
    ErrExecutionFailed    = errors.New("execution failed")
    ErrResourceExhausted  = errors.New("resource limit exceeded")
)

// Executor manages task execution for off-chain workers
type Executor struct {
    runtime      *runtime.WasmRuntime
    worker       *OffChainWorker
    config       *ExecutorConfig
    log          logging.Logger

    // Task management
    tasks        chan Task
    results      chan Result
    active       sync.Map
    metrics      *ExecutorMetrics

    // Resource management
    memoryUsage  uint64
    lock         sync.RWMutex

    // Lifecycle management
    ctx          context.Context
    cancel       context.CancelFunc
    wg           sync.WaitGroup
}

type ExecutorConfig struct {
    MaxConcurrentTasks int
    TaskQueueSize      int
    ResultBufferSize   int
    MaxMemoryPerTask   uint64
    MaxFuelPerTask     uint64
    TaskTimeout        time.Duration
    EnableMetrics      bool
}

type ExecutorMetrics struct {
    TasksProcessed    uint64
    TasksFailed       uint64
    TotalExecutionTime time.Duration
    MemoryHighWater   uint64
    FuelConsumed      uint64
    lock              sync.Mutex
}

// NewExecutor creates a new task executor
func NewExecutor(
    runtime *runtime.WasmRuntime,
    worker *OffChainWorker,
    config *ExecutorConfig,
    log logging.Logger,
) *Executor {
    ctx, cancel := context.WithCancel(context.Background())

    executor := &Executor{
        runtime:  runtime,
        worker:   worker,
        config:   config,
        log:      log,
        tasks:    make(chan Task, config.TaskQueueSize),
        results:  make(chan Result, config.ResultBufferSize),
        metrics:  &ExecutorMetrics{},
        ctx:      ctx,
        cancel:   cancel,
    }

    return executor
}

// Start begins task execution
func (e *Executor) Start() error {
    e.log.Info("Starting off-chain executor")

    for i := 0; i < e.config.MaxConcurrentTasks; i++ {
        e.wg.Add(1)
        go e.processTaskLoop()
    }

    // Start metrics collection if enabled
    if e.config.EnableMetrics {
        e.wg.Add(1)
        go e.collectMetrics()
    }

    return nil
}

// Stop gracefully shuts down the executor
func (e *Executor) Stop() error {
    e.log.Info("Stopping off-chain executor")
    e.cancel()
    e.wg.Wait()
    return nil
}

// Submit adds a task for execution
func (e *Executor) Submit(task Task) error {
    if err := e.validateTask(task); err != nil {
        return fmt.Errorf("%w: %v", ErrInvalidTask, err)
    }

    select {
    case e.tasks <- task:
        return nil
    case <-e.ctx.Done():
        return ErrExecutorStopped
    default:
        return ErrExecutorBusy
    }
}

func (e *Executor) processTaskLoop() {
    defer e.wg.Done()

    for {
        select {
        case task := <-e.tasks:
            result := e.executeTask(task)
            e.handleResult(result)
        case <-e.ctx.Done():
            return
        }
    }
}

func (e *Executor) executeTask(task Task) Result {
    startTime := time.Now()
    taskCtx, cancel := context.WithTimeout(e.ctx, e.config.TaskTimeout)
    defer cancel()

    // Track active task
    taskID := generateTaskID(task)
    e.active.Store(taskID, struct{}{})
    defer e.active.Delete(taskID)

    // Prepare execution context
    callInfo := &runtime.CallInfo{
        Contract:     task.Contract,
        FunctionName: task.Function,
        Params:       task.Params,
        State:        e.worker.chainState,
        Height:       task.BlockContext.Height,
        Timestamp:    task.BlockContext.Timestamp,
        Fuel:         e.config.MaxFuelPerTask,
    }

    // Execute task with resource monitoring
    resourceCtx := e.trackResources(taskCtx, taskID)
    output, err := e.runtime.CallContract(resourceCtx, callInfo)

    // Create result
    result := Result{
        TaskID:        taskID,
        Output:        output,
        Error:         err,
        ExecutionTime: time.Since(startTime),
        ResourceStats: e.collectTaskStats(taskID, callInfo),
    }

    // Process any generated transactions if execution was successful
    if err == nil && output != nil && len(output.Outputs) > 0 {
        txs, txErr := e.processTransactions(output.Outputs)
        if txErr != nil {
            e.log.Warn("Failed to process transactions: %v", txErr)
        }
        result.GeneratedTxs = txs
    }

    return result
}

func (e *Executor) handleResult(result Result) {
    // Update metrics
    if e.config.EnableMetrics {
        e.updateMetrics(result)
    }

    // Send result
    select {
    case e.results <- result:
    case <-e.ctx.Done():
        return
    default:
        e.log.Warn("Result channel full, dropping result for task %d", result.TaskID)
    }
}

func (e *Executor) trackResources(ctx context.Context, taskID uint64) context.Context {
    resourceCtx, cancel := context.WithCancel(ctx)
    
    go func() {
        ticker := time.NewTicker(100 * time.Millisecond)
        defer ticker.Stop()

        for {
            select {
            case <-ticker.C:
                if e.checkResourceLimits(taskID) {
                    cancel()
                    return
                }
            case <-resourceCtx.Done():
                return
            }
        }
    }()

    return resourceCtx
}

func (e *Executor) checkResourceLimits(taskID uint64) bool {
    e.lock.RLock()
    defer e.lock.RUnlock()

    // Check memory usage
    if e.memoryUsage > e.config.MaxMemoryPerTask {
        e.log.Warn("Task %d exceeded memory limit", taskID)
        return true
    }

    return false
}

func (e *Executor) collectTaskStats(taskID uint64, callInfo *runtime.CallInfo) ResourceStats {
    return ResourceStats{
        MemoryUsed:   e.getTaskMemoryUsage(taskID),
        FuelConsumed: e.config.MaxFuelPerTask - callInfo.RemainingFuel(),
    }
}

func (e *Executor) processTransactions(outputs [][]byte) ([]*chain.Transaction, error) {
    var txs []*chain.Transaction

    for _, output := range outputs {
        tx, err := chain.UnmarshalTransaction(output)
        if err != nil {
            continue // Skip invalid transaction data
        }
        
        // Verify transaction
        if err := e.validateTransaction(tx); err != nil {
            e.log.Warn("Invalid transaction generated: %v", err)
            continue
        }

        txs = append(txs, tx)
    }

    return txs, nil
}

func (e *Executor) validateTransaction(tx *chain.Transaction) error {
    // Add transaction validation logic
    return nil
}

func (e *Executor) validateTask(task Task) error {
    if task.Contract == codec.EmptyAddress {
        return errors.New("invalid contract address")
    }

    if task.Function == "" {
        return errors.New("empty function name")
    }

    if task.BlockContext.Height == 0 {
        return errors.New("invalid block height")
    }

    return nil
}

func (e *Executor) getTaskMemoryUsage(taskID uint64) uint64 {
    e.lock.RLock()
    defer e.lock.RUnlock()
    // Implementation would track per-task memory usage
    return 0
}

func (e *Executor) updateMetrics(result Result) {
    e.metrics.lock.Lock()
    defer e.metrics.lock.Unlock()

    e.metrics.TasksProcessed++
    if result.Error != nil {
        e.metrics.TasksFailed++
    }
    e.metrics.TotalExecutionTime += result.ExecutionTime
    e.metrics.FuelConsumed += result.ResourceStats.FuelConsumed

    if result.ResourceStats.MemoryUsed > e.metrics.MemoryHighWater {
        e.metrics.MemoryHighWater = result.ResourceStats.MemoryUsed
    }
}

func (e *Executor) collectMetrics() {
    defer e.wg.Done()

    ticker := time.NewTicker(time.Minute)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            e.logMetrics()
        case <-e.ctx.Done():
            return
        }
    }
}

func (e *Executor) logMetrics() {
    e.metrics.lock.Lock()
    defer e.metrics.lock.Unlock()

    e.log.Info("Executor metrics: processed=%d failed=%d avgTime=%v memHigh=%d fuel=%d",
        e.metrics.TasksProcessed,
        e.metrics.TasksFailed,
        e.metrics.TotalExecutionTime/time.Duration(e.metrics.TasksProcessed),
        e.metrics.MemoryHighWater,
        e.metrics.FuelConsumed,
    )
}

func generateTaskID(task Task) uint64 {
    // Implementation would generate unique task ID
    return 0
}

// GetResults returns the channel for receiving execution results
func (e *Executor) GetResults() <-chan Result {
    return e.results
}

// GetMetrics returns current executor metrics
func (e *Executor) GetMetrics() ExecutorMetrics {
    e.metrics.lock.Lock()
    defer e.metrics.lock.Unlock()
    return *e.metrics
}