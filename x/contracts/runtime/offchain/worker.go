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
    "github.com/ava-labs/hypersdk/runtime"
    "github.com/ava-labs/hypersdk/runtime/events"
    "github.com/ava-labs/hypersdk/state"
)

var (
    ErrWorkerStopped      = errors.New("worker stopped")
    ErrExecutionTimeout   = errors.New("execution timeout")
    ErrInvalidBlockHeight = errors.New("invalid block height")
    ErrTaskQueueFull      = errors.New("task queue full")
    ErrReadOnlyViolation  = errors.New("attempted write in read-only context")
)

// OffChainWorker manages off-chain computation tasks
type OffChainWorker struct {
    // Core components
    runtime      *runtime.WasmRuntime
    chainState   runtime.StateManager
    localState   state.Mutable
    txPool       TransactionPool
    eventManager *events.Manager
    log         logging.Logger

    // Configuration
    config       *WorkerConfig

    // Task management
    taskQueue    chan Task
    results      chan Result
    workerPool   chan struct{}

    // State management
    currentBlock uint64
    lock         sync.RWMutex

    // Lifecycle management
    ctx          context.Context
    cancel       context.CancelFunc
    wg           sync.WaitGroup
}

// WorkerConfig defines configuration for off-chain workers
type WorkerConfig struct {
    MaxExecutionTime    time.Duration
    MaxMemory          uint64
    WorkerCount        int
    TaskQueueSize      int
    ResultBufferSize   int
    LocalStoragePath   string
    RetryAttempts      int
    RetryDelay         time.Duration
}

// DefaultWorkerConfig returns default configuration
func DefaultWorkerConfig() *WorkerConfig {
    return &WorkerConfig{
        MaxExecutionTime:  30 * time.Second,
        MaxMemory:        1 << 30, // 1GB
        WorkerCount:      4,
        TaskQueueSize:    1000,
        ResultBufferSize: 1000,
        RetryAttempts:    3,
        RetryDelay:       time.Second,
    }
}

// Task represents an off-chain computation request
type Task struct {
    Contract     codec.Address
    Function     string
    Params       []byte
    BlockContext BlockContext
    Priority     uint8
    RetryCount   int
}

// BlockContext provides chain context for off-chain execution
type BlockContext struct {
    Height      uint64
    Hash        ids.ID
    ParentHash  ids.ID
    Timestamp   uint64
}

// Result represents the outcome of an off-chain computation
type Result struct {
    TaskID            uint64
    Output            *chain.Result
    GeneratedTxs      []*chain.Transaction
    Error             error
    ExecutionTime     time.Duration
    ResourceStats     ResourceStats
}

// ResourceStats tracks resource usage during execution
type ResourceStats struct {
    MemoryUsed    uint64
    FuelConsumed  uint64
    StorageRead   uint64
    StorageWrite  uint64
}

// NewOffChainWorker creates a new off-chain worker instance
func NewOffChainWorker(
    cfg *WorkerConfig,
    runtime *runtime.WasmRuntime,
    chainState runtime.StateManager,
    localState state.Mutable,
    txPool TransactionPool,
    log logging.Logger,
) *OffChainWorker {
    ctx, cancel := context.WithCancel(context.Background())
    
    return &OffChainWorker{
        runtime:      runtime,
        chainState:   chainState,
        localState:   localState,
        txPool:       txPool,
        log:         log,
        config:      cfg,
        taskQueue:   make(chan Task, cfg.TaskQueueSize),
        results:     make(chan Result, cfg.ResultBufferSize),
        workerPool:  make(chan struct{}, cfg.WorkerCount),
        ctx:         ctx,
        cancel:      cancel,
    }
}

// Start begins worker operation
func (w *OffChainWorker) Start() error {
    w.log.Info("Starting off-chain worker")
    
    // Initialize worker pool
    for i := 0; i < w.config.WorkerCount; i++ {
        w.workerPool <- struct{}{}
    }

    // Start task processor
    w.wg.Add(1)
    go w.processTaskQueue()

    return nil
}

// Stop gracefully shuts down the worker
func (w *OffChainWorker) Stop() error {
    w.log.Info("Stopping off-chain worker")
    w.cancel()
    w.wg.Wait()
    return nil
}

// ScheduleTask submits a new task for execution
func (w *OffChainWorker) ScheduleTask(task Task) error {
    if err := w.validateTask(task); err != nil {
        return err
    }

    select {
    case w.taskQueue <- task:
        return nil
    case <-w.ctx.Done():
        return ErrWorkerStopped
    default:
        return ErrTaskQueueFull
    }
}

// GetResults returns a channel for receiving task results
func (w *OffChainWorker) GetResults() <-chan Result {
    return w.results
}

// UpdateBlockContext updates the current block context
func (w *OffChainWorker) UpdateBlockContext(height uint64, hash ids.ID) {
    w.lock.Lock()
    defer w.lock.Unlock()
    w.currentBlock = height
}

func (w *OffChainWorker) processTaskQueue() {
    defer w.wg.Done()

    for {
        select {
        case task := <-w.taskQueue:
            // Acquire worker from pool
            select {
            case <-w.workerPool:
                w.wg.Add(1)
                go w.executeTask(task)
            case <-w.ctx.Done():
                return
            }
        case <-w.ctx.Done():
            return
        }
    }
}

func (w *OffChainWorker) executeTask(task Task) {
    defer func() {
        w.workerPool <- struct{} // Return worker to pool
        w.wg.Done()
    }()

    start := time.Now()
    
    // Create execution context with timeout
    execCtx, cancel := context.WithTimeout(w.ctx, w.config.MaxExecutionTime)
    defer cancel()

    // Execute task
    result := w.doExecuteTask(execCtx, task)
    result.ExecutionTime = time.Since(start)

    // Send result
    select {
    case w.results <- result:
    case <-w.ctx.Done():
        return
    }
}

func (w *OffChainWorker) doExecuteTask(ctx context.Context, task Task) Result {
    // Create call info for read-only execution
    callInfo := &runtime.CallInfo{
        Contract:     task.Contract,
        FunctionName: task.Function,
        Params:      task.Params,
        State:       w.createReadOnlyState(),
        Height:      task.BlockContext.Height,
        Timestamp:   task.BlockContext.Timestamp,
    }

    // Execute contract
    output, err := w.runtime.CallContract(ctx, callInfo)
    if err != nil {
        return Result{
            Error: fmt.Errorf("contract execution failed: %w", err),
        }
    }

    // Process any generated transactions
    var txs []*chain.Transaction
    if len(output.Outputs) > 0 {
        txs, err = w.processContractOutput(output.Outputs)
        if err != nil {
            return Result{
                Error: fmt.Errorf("failed to process contract output: %w", err),
            }
        }
    }

    return Result{
        Output:       output,
        GeneratedTxs: txs,
        ResourceStats: ResourceStats{
            FuelConsumed: w.config.MaxExecutionTime - callInfo.RemainingFuel(),
        },
    }
}

func (w *OffChainWorker) createReadOnlyState() runtime.StateManager {
    return NewReadOnlyStateManager(w.chainState)
}

func (w *OffChainWorker) processContractOutput(outputs [][]byte) ([]*chain.Transaction, error) {
    var txs []*chain.Transaction
    
    for _, output := range outputs {
        tx, err := chain.UnmarshalTransaction(output)
        if err != nil {
            continue // Skip invalid transaction data
        }
        txs = append(txs, tx)
    }
    
    return txs, nil
}

func (w *OffChainWorker) validateTask(task Task) error {
    w.lock.RLock()
    defer w.lock.RUnlock()

    if task.BlockContext.Height > w.currentBlock {
        return ErrInvalidBlockHeight
    }

    if task.Contract == codec.EmptyAddress {
        return errors.New("invalid contract address")
    }

    if task.Function == "" {
        return errors.New("empty function name")
    }

    return nil
}

// ReadOnlyStateManager wraps a StateManager to enforce read-only access
type ReadOnlyStateManager struct {
    inner runtime.StateManager
}

func NewReadOnlyStateManager(inner runtime.StateManager) *ReadOnlyStateManager {
    return &ReadOnlyStateManager{inner: inner}
}

func (r *ReadOnlyStateManager) GetValue(ctx context.Context, key []byte) ([]byte, error) {
    return r.inner.GetValue(ctx, key)
}

func (r *ReadOnlyStateManager) SetValue(ctx context.Context, key []byte, value []byte) error {
    return ErrReadOnlyViolation
}

// TransactionPool interface for submitting transactions
type TransactionPool interface {
    AddLocal(tx *chain.Transaction) error
}