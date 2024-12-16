package offchain

import (
    "context"
    "errors"
    "fmt"
    "sync"
    "sync/atomic"
    "time"

    "github.com/ava-labs/avalanchego/utils/logging"
    "github.com/ava-labs/hypersdk/chain"
    "github.com/ava-labs/hypersdk/codec"
    "github.com/ava-labs/hypersdk/x/contracts/runtime"
    "github.com/ava-labs/hypersdk/x/contracts/runtime/events"
)

var (
    ErrExecutorStopped    = errors.New("executor stopped")
    ErrExecutionTimeout   = errors.New("execution timeout")
    ErrResourceExhausted  = errors.New("resource limit exceeded")
    ErrInvalidTx         = errors.New("invalid transaction")
)

type Task struct {
    Contract     codec.Address
    Function     string
    Params       []byte
    BlockContext BlockContext
}

type BlockContext struct {
    Height    uint64
    Hash      []byte
    Timestamp uint64
}

type Result struct {
    TaskID        uint64
    Output        *chain.Result
    GeneratedTxs  []*chain.Transaction
    Events        []events.Event
    ExecutionTime time.Duration
    ResourceStats *ResourceStats
    Error         error
}

type ResourceStats struct {
    MemoryUsed   uint64
    FuelConsumed uint64
}

type Worker struct {
    ID         string
    ChainState runtime.StateManager
}

type Executor struct {
    runtime      *runtime.WasmRuntime
    worker       *Worker
    config       *ExecutorConfig
    log          logging.Logger

    tasks        chan Task
    results      chan Result
    active       sync.Map
    metrics      *ExecutorMetrics

    memoryUsage  uint64
    lock         sync.RWMutex

    eventManager *events.Manager

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
    TasksProcessed     uint64
    TasksFailed        uint64
    TotalExecutionTime time.Duration
    MemoryHighWater    uint64
    FuelConsumed       uint64
    EventsProcessed    uint64
    lock               sync.Mutex
}

func NewExecutor(
    runtime *runtime.WasmRuntime,
    worker *Worker,
    config *ExecutorConfig,
    log logging.Logger,
) (*Executor, error) {
    ctx, cancel := context.WithCancel(context.Background())

    executor := &Executor{
        runtime:      runtime,
        worker:       worker,
        config:       config,
        log:         log,
        tasks:       make(chan Task, config.TaskQueueSize),
        results:     make(chan Result, config.ResultBufferSize),
        metrics:     &ExecutorMetrics{},
        eventManager: runtime.Events(),
        ctx:         ctx,
        cancel:      cancel,
    }

    return executor, nil
}

func (e *Executor) Start() error {
    e.log.Info(fmt.Sprintf("Starting executor with %d workers", e.config.MaxConcurrentTasks))

    for i := 0; i < e.config.MaxConcurrentTasks; i++ {
        e.wg.Add(1)
        go e.processTaskLoop()
    }

    if e.config.EnableMetrics {
        e.wg.Add(1)
        go e.collectMetrics()
    }

    return nil
}

func (e *Executor) Stop() error {
    e.log.Info("Stopping executor")
    e.cancel()
    e.wg.Wait()
    return nil
}

func (e *Executor) Submit(task Task) error {
    if err := e.validateTask(task); err != nil {
        return fmt.Errorf("invalid task: %w", err)
    }

    select {
    case e.tasks <- task:
        return nil
    case <-e.ctx.Done():
        return ErrExecutorStopped
    default:
        return ErrResourceExhausted
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

    taskID := generateTaskID()
    e.active.Store(taskID, struct{}{})
    defer e.active.Delete(taskID)

    callInfo := &runtime.CallInfo{
        Contract:     task.Contract,
        FunctionName: task.Function,
        Params:       task.Params,
        State:        e.worker.ChainState,
        Height:       task.BlockContext.Height,
        Timestamp:    task.BlockContext.Timestamp,
        Fuel:         e.config.MaxFuelPerTask,
    }

    resourceCtx := e.trackResources(taskCtx, taskID)
    output, err := e.runtime.CallContract(resourceCtx, callInfo)

    if err != nil {
        return Result{
            TaskID:        taskID,
            Error:         fmt.Errorf("execution failed: %w", err),
            ExecutionTime: time.Since(startTime),
            ResourceStats: e.collectTaskStats(taskID, callInfo),
        }
    }

    // Process events
    if output.Success {
        for _, eventData := range output.Outputs {
            var evt events.Event
            if err := codec.Marshal(eventData, &evt); err == nil {
                if err := e.validateEvent(evt, task.BlockContext); err == nil {
                    output, err = e.eventManager.EmitWithResult(evt, output)
                    if err != nil {
                        e.log.Info(fmt.Sprintf("Failed to emit event: %v", err))
                    } else {
                        e.updateEventMetrics()
                    }
                }
            }
        }
    }

    // Process transactions
    var txs []*chain.Transaction
    if output.Success && len(output.Outputs) > 0 {
        txs, err = e.processTransactions(output.Outputs)
        if err != nil {
            e.log.Info(fmt.Sprintf("Failed to process transactions: %v", err))
        }
    }

    return Result{
        TaskID:        taskID,
        Output:        output,
        GeneratedTxs:  txs,
        Events:        callInfo.GetEvents(), // Assuming GetEvents() method exists
        ExecutionTime: time.Since(startTime),
        ResourceStats: e.collectTaskStats(taskID, callInfo),
    }
}

func (e *Executor) processTransactions(outputs [][]byte) ([]*chain.Transaction, error) {
    var txs []*chain.Transaction

    for _, output := range outputs {
        tx := new(chain.Transaction)
        if err := codec.Marshal(output, tx); err != nil {
            continue
        }
        
        if err := e.validateTransaction(tx); err != nil {
            e.log.Info(fmt.Sprintf("Invalid transaction generated: %v", err))
            continue
        }

        txs = append(txs, tx)
    }

    return txs, nil
}

func (e *Executor) validateEvent(evt events.Event, blockCtx BlockContext) error {
    if evt.BlockHeight > blockCtx.Height {
        return fmt.Errorf("event height %d exceeds block height %d", 
            evt.BlockHeight, blockCtx.Height)
    }
    return nil
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

    if e.memoryUsage > e.config.MaxMemoryPerTask {
        e.log.Info(fmt.Sprintf("Task %d exceeded memory limit", taskID))
        return true
    }

    return false
}

func (e *Executor) collectTaskStats(taskID uint64, callInfo *runtime.CallInfo) *ResourceStats {
    return &ResourceStats{
        MemoryUsed:   e.getTaskMemoryUsage(taskID),
        FuelConsumed: e.config.MaxFuelPerTask - callInfo.RemainingFuel(),
    }
}

func (e *Executor) handleResult(result Result) {
    e.metrics.lock.Lock()
    if result.Error != nil {
        e.metrics.TasksFailed++
    } else {
        e.metrics.TasksProcessed++
    }
    e.metrics.TotalExecutionTime += result.ExecutionTime
    e.metrics.lock.Unlock()

    select {
    case e.results <- result:
    case <-e.ctx.Done():
        return
    }
}

func (e *Executor) validateTask(task Task) error {
    if task.Contract == codec.EmptyAddress {
        return errors.New("invalid contract address")
    }

    if task.Function == "" {
        return errors.New("empty function name")
    }

    return nil
}

func (e *Executor) validateTransaction(tx *chain.Transaction) error {
    if tx == nil {
        return ErrInvalidTx
    }
    return nil
}

func (e *Executor) getTaskMemoryUsage(taskID uint64) uint64 {
    e.lock.RLock()
    defer e.lock.RUnlock()
    return 0 // Implementation would track actual memory usage
}

func (e *Executor) updateEventMetrics() {
    e.metrics.lock.Lock()
    defer e.metrics.lock.Unlock()
    e.metrics.EventsProcessed++
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

    e.log.Info(fmt.Sprintf(
        "Executor metrics: processed=%d failed=%d avgTime=%v events=%d memHigh=%d fuel=%d",
        e.metrics.TasksProcessed,
        e.metrics.TasksFailed,
        e.metrics.TotalExecutionTime/time.Duration(e.metrics.TasksProcessed),
        e.metrics.EventsProcessed,
        e.metrics.MemoryHighWater,
        e.metrics.FuelConsumed,
    ))
}

func (e *Executor) GetResults() <-chan Result {
    return e.results
}

func (e *Executor) GetMetrics() ExecutorMetrics {
    e.metrics.lock.Lock()
    defer e.metrics.lock.Unlock()
    return *e.metrics
}

var taskIDCounter uint64

func generateTaskID() uint64 {
    return atomic.AddUint64(&taskIDCounter, 1)
}