package offchain

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/hypersdk/chain"
	"github.com/ava-labs/hypersdk/codec"
	"github.com/ava-labs/hypersdk/runtime"
	"github.com/ava-labs/hypersdk/state"
	"github.com/ava-labs/hypersdk/x/contracts/runtime/events"
)

type OffChainWorker struct {
    // Core components
    runtime      *runtime.WasmRuntime
    chainState   runtime.StateManager
    localState   state.Mutable    
    txPool       TransactionPool
    eventManager *events.Manager
    log         logging.Logger
    config      *WorkerConfig

    // Coordination components
    coordinator  *WorkerCoordinator
    workerID     WorkerID
    taskQueue    chan Task
    results      chan Result
    
    // Task management
    currentBlock uint64
    lock         sync.RWMutex

    // Lifecycle management
    ctx          context.Context
    cancel       context.CancelFunc
    wg           sync.WaitGroup
}

type WorkerConfig struct {
    // Existing config
    MaxExecutionTime    time.Duration
    MaxMemory          uint64
    WorkerCount        int
    TaskQueueSize      int
    ResultBufferSize   int
    LocalStoragePath   string
    RetryAttempts      int
    RetryDelay         time.Duration

    // Coordination config
    CoordinationTimeout time.Duration
}

func NewOffChainWorker(
    cfg *WorkerConfig,
    runtime *runtime.WasmRuntime,
    chainState runtime.StateManager,
    localState state.Mutable,
    txPool TransactionPool,
    log logging.Logger,
) (*OffChainWorker, error) {
    ctx, cancel := context.WithCancel(context.Background())

    worker := &OffChainWorker{
        runtime:     runtime,
        chainState:  chainState,
        localState:  localState,
        txPool:      txPool,
        log:        log,
        config:     cfg,

        // Initialize coordination components
        workerID:    WorkerID(ids.GenerateTestID().String()),
        taskQueue:   make(chan Task, cfg.TaskQueueSize),
        results:     make(chan Result, cfg.ResultBufferSize),
        coordinator: NewWorkerCoordinator(cfg.WorkerCount),

        ctx:        ctx,
        cancel:     cancel,
    }

    // Start coordination handler
    go worker.handleCoordination()

    return worker, nil
}

func (w *OffChainWorker) Start() error {
    w.log.Info("Starting off-chain worker")
    
    // Start task processor
    w.wg.Add(1)
    go w.processTaskQueue()

    return nil
}

func (w *OffChainWorker) Stop() error {
    w.log.Info("Stopping off-chain worker")
    w.cancel()
    w.wg.Wait()
    return nil
}

func (w *OffChainWorker) handleCoordination() {
    for {
        select {
        case task := <-w.taskQueue:
            if task.RequiresCoordination() {
                w.handleCoordinatedTask(task)
            } else {
                w.executeTask(task)
            }
        case result := <-w.results:
            w.handleResult(result)
        case <-w.ctx.Done():
            return
        }
    }
}

func (w *OffChainWorker) handleCoordinatedTask(task Task) error {
    return w.coordinator.CoordinateTask(TaskWithCoordination{
        Task:         task,
        Timeout:      w.config.CoordinationTimeout,
        EnclaveID:    w.workerID,
    })
}

func (w *OffChainWorker) executeTask(task Task) error {
    if task.RequiresCoordination() {
        coord := task.GetCoordination()
        if err := w.synchronizeWithPartner(coord); err != nil {
            return err
        }
    }

    callInfo := &runtime.CallInfo{
        Contract:     task.Contract,
        Function:     task.Function,
        Params:       task.Params,
        State:       w.createReadOnlyState(),
        Height:      w.currentBlock,
    }

    result, err := w.runtime.CallContract(context.Background(), callInfo)
    if err != nil {
        return fmt.Errorf("contract execution failed: %w", err)
    }

    if task.RequiresCoordination() {
        if err := w.handleCoordinatedResult(result, task); err != nil {
            return err
        }
    }

    // Send result
    w.results <- Result{
        TaskID:  task.ID,
        Output:  result,
        Error:   err,
    }

    return nil
}

func (w *OffChainWorker) synchronizeWithPartner(coord *TaskCoordination) error {
    switch coord.WorkerIndex {
    case 0:
        return w.initiateSynchronization(coord)
    case 1:
        return w.respondToSynchronization(coord)
    default:
        return ErrInvalidWorkerIndex
    }
}

func (w *OffChainWorker) initiateSynchronization(coord *TaskCoordination) error {
    coord.SyncPoint <- struct{}{}
    
    select {
    case <-coord.SyncPoint:
        return nil
    case <-time.After(w.config.CoordinationTimeout):
        return ErrCoordinationTimeout
    }
}

func (w *OffChainWorker) respondToSynchronization(coord *TaskCoordination) error {
    select {
    case <-coord.SyncPoint:
        coord.SyncPoint <- struct{}{}
        return nil
    case <-time.After(w.config.CoordinationTimeout):
        return ErrCoordinationTimeout
    }
}

func (w *OffChainWorker) handleCoordinatedResult(result *chain.Result, task Task) error {
    coord := task.GetCoordination()
    
    // Send result to coordination channel
    select {
    case coord.ResultChan <- CoordinatedResult{
        WorkerID: w.workerID,
        Result:   result,
    }:
        return nil
    case <-time.After(w.config.CoordinationTimeout):
        return ErrCoordinationTimeout
    }
}

func (w *OffChainWorker) handleResult(result Result) {
    // Handle normal result processing
    if err := w.processResult(result); err != nil {
        w.log.Error("Failed to process result: %v", err)
    }
}

func (w *OffChainWorker) processResult(result Result) error {
    // Implement result processing logic
    return nil
}

func (w *OffChainWorker) createReadOnlyState() runtime.StateManager {
    return NewReadOnlyStateManager(w.chainState)
}

func (w *OffChainWorker) UpdateBlockContext(height uint64, hash ids.ID) {
    w.lock.Lock()
    defer w.lock.Unlock()
    w.currentBlock = height
}

// Preserved helper methods
func (w *OffChainWorker) ScheduleTask(task Task) error {
    select {
    case w.taskQueue <- task:
        return nil
    case <-w.ctx.Done():
        return ErrWorkerStopped
    default:
        return ErrTaskQueueFull
    }
}

func (w *OffChainWorker) processTaskQueue() {
    defer w.wg.Done()

    for {
        select {
        case task := <-w.taskQueue:
            if err := w.executeTask(task); err != nil {
                w.log.Error("Task execution failed: %v", err)
            }
        case <-w.ctx.Done():
            return
        }
    }
}

// Types needed for coordination
type Result struct {
    TaskID  string
    Output  *chain.Result
    Error   error
}

type CoordinatedResult struct {
    WorkerID WorkerID
    Result   *chain.Result
    Error    error
}