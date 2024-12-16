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
    "go.uber.org/zap"
)

var (
    ErrInvalidTransaction    = errors.New("invalid transaction")
    ErrTransactionRejected   = errors.New("transaction rejected")
    ErrTxPoolFull           = errors.New("transaction pool full")
    ErrDuplicateTransaction = errors.New("duplicate transaction")
    ErrInsufficientFunds    = errors.New("insufficient funds")
)

const (
    // Maximum size of a transaction in bytes
    MaxTransactionSize = 1 << 20 // 1MB
)

// TransactionSubmitter handles transaction generation and submission
type TransactionSubmitter struct {
    txPool      TxPool
    signer      Signer
    state       *OffChainState
    log         logging.Logger
    config      *TxSubmitterConfig

    // Transaction tracking
    pending     map[ids.ID]*PendingTx
    lock        sync.RWMutex

    // Metrics
    metrics     *TxMetrics

    // Batch processing
    batch       []*chain.Transaction
    batchLock   sync.Mutex

    // Lifecycle
    ctx         context.Context
    cancel      context.CancelFunc
    wg          sync.WaitGroup
}

type TxSubmitterConfig struct {
    MaxPendingTxs    int
    BatchSize        int
    BatchTimeout     time.Duration
    RetryAttempts    int
    RetryDelay       time.Duration
    EnableBatching   bool
}

type PendingTx struct {
    Tx          *chain.Transaction
    SubmitTime  time.Time
    Attempts    int
    LastAttempt time.Time
}

type TxMetrics struct {
    Submitted    uint64
    Accepted     uint64
    Rejected     uint64
    Retried      uint64
    AvgGasPrice  uint64
    TotalFees    uint64
    lock         sync.Mutex
}

// Signer interface for transaction signing
type Signer interface {
    SignTransaction(*chain.Transaction) (*chain.Transaction, error)
}

// NewTransactionSubmitter creates a new transaction submitter
func NewTransactionSubmitter(
    txPool TxPool,
    signer Signer,  // Changed from crypto.Signer
    state *OffChainState,
    config *TxSubmitterConfig,
    log logging.Logger,
) *TransactionSubmitter {
    ctx, cancel := context.WithCancel(context.Background())

    return &TransactionSubmitter{
        txPool:   txPool,
        signer:   signer,
        state:    state,
        config:   config,
        log:      log,
        pending:  make(map[ids.ID]*PendingTx),
        metrics:  &TxMetrics{},
        batch:    make([]*chain.Transaction, 0, config.BatchSize),
        ctx:      ctx,
        cancel:   cancel,
    }
}

// Start begins transaction processing
func (t *TransactionSubmitter) Start() error {
    t.wg.Add(1)
    go t.processBatchLoop()

    t.wg.Add(1)
    go t.monitorPendingTxs()

    return nil
}

// Stop gracefully shuts down the submitter
func (t *TransactionSubmitter) Stop() error {
    t.cancel()
    t.wg.Wait()
    return nil
}

// Submit submits a transaction to the network
func (t *TransactionSubmitter) Submit(tx *chain.Transaction) error {
    if err := t.validateTransaction(tx); err != nil {
        return fmt.Errorf("%w: %v", ErrInvalidTransaction, err)
    }

    if t.config.EnableBatching {
        return t.addToBatch(tx)
    }

    return t.submitSingle(tx)
}

// SubmitBatch submits a batch of transactions
func (t *TransactionSubmitter) SubmitBatch(txs []*chain.Transaction) error {
    for _, tx := range txs {
        if err := t.validateTransaction(tx); err != nil {
            return fmt.Errorf("%w: %v", ErrInvalidTransaction, err)
        }
    }

    return t.submitBatch(txs)
}

func (t *TransactionSubmitter) submitSingle(tx *chain.Transaction) error {
    // Sign transaction if not already signed
    if (!tx.IsSigned()) {
        signedTx, err := t.signTransaction(tx)
        if err != nil {
            return err
        }
        tx = signedTx
    }

    // Submit to tx pool
    if err := t.txPool.AddLocal(tx); err != nil {
        return fmt.Errorf("%w: %v", ErrTransactionRejected, err)
    }

    // Track pending transaction
    t.trackPendingTx(tx)

    // Update metrics
    t.updateMetrics(tx)

    return nil
}

func (t *TransactionSubmitter) submitBatch(txs []*chain.Transaction) error {
    signedTxs := make([]*chain.Transaction, 0, len(txs))

    // Sign all transactions
    for _, tx := range txs {
        if !tx.IsSigned() {
            signedTx, err := t.signTransaction(tx)
            if err != nil {
                return err
            }
            signedTxs = append(signedTxs, signedTx)
        } else {
            signedTxs = append(signedTxs, tx)
        }
    }

    // Submit batch to tx pool
    if err := t.txPool.AddLocalBatch(signedTxs); err != nil {
        return fmt.Errorf("%w: %v", ErrTransactionRejected, err)
    }

    // Track pending transactions
    for _, tx := range signedTxs {
        t.trackPendingTx(tx)
    }

    // Update metrics
    for _, tx := range signedTxs {
        t.updateMetrics(tx)
    }

    return nil
}

func (t *TransactionSubmitter) addToBatch(tx *chain.Transaction) error {
    t.batchLock.Lock()
    defer t.batchLock.Unlock()

    t.batch = append(t.batch, tx)

    // Submit batch if full
    if len(t.batch) >= t.config.BatchSize {
        return t.submitBatch(t.batch)
    }

    return nil
}

func (t *TransactionSubmitter) processBatchLoop() {
    defer t.wg.Done()

    ticker := time.NewTicker(t.config.BatchTimeout)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            t.submitPendingBatch()
        case <-t.ctx.Done():
            return
        }
    }
}

func (t *TransactionSubmitter) submitPendingBatch() {
    t.batchLock.Lock()
    defer t.batchLock.Unlock()

    if len(t.batch) == 0 {
        return
    }

    if err := t.submitBatch(t.batch); err != nil {
        t.log.Error(fmt.Sprintf("Failed to submit batch: %v", err))
    }

    // Clear batch
    t.batch = make([]*chain.Transaction, 0, t.config.BatchSize)
}

func (t *TransactionSubmitter) monitorPendingTxs() {
    defer t.wg.Done()

    ticker := time.NewTicker(time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            t.checkPendingTxs()
        case <-t.ctx.Done():
            return
        }
    }
}

func (t *TransactionSubmitter) checkPendingTxs() {
    t.lock.Lock()
    defer t.lock.Unlock()

    now := time.Now()
    for txID, pending := range t.pending {
        // Check if transaction has been pending too long
        if now.Sub(pending.SubmitTime) > t.config.BatchTimeout {
            if pending.Attempts < t.config.RetryAttempts {
                // Retry submission
                if err := t.retryTransaction(pending.Tx); err != nil {
                    t.log.Warn(fmt.Sprintf("Failed to retry transaction %s: %v", txID, err))
                }
                
                pending.Attempts++
                pending.LastAttempt = now
            } else {
                // Give up on transaction
                t.log.Warn("Transaction warning", zap.Stringer("txID", txID))
                delete(t.pending, txID)
            }
        }
    }
}

func (t *TransactionSubmitter) retryTransaction(tx *chain.Transaction) error {
    t.metrics.lock.Lock()
    t.metrics.Retried++
    t.metrics.lock.Unlock()

    return t.txPool.AddLocal(tx)
}

func (t *TransactionSubmitter) trackPendingTx(tx *chain.Transaction) {
    t.lock.Lock()
    defer t.lock.Unlock()

    t.pending[tx.ID()] = &PendingTx{
        Tx:         tx,
        SubmitTime: time.Now(),
        Attempts:   1,
    }
}

func (t *TransactionSubmitter) validateTransaction(tx *chain.Transaction) error {
    // Basic validation
    if tx == nil {
        return errors.New("nil transaction")
    }

    // Validate size - using local constant instead of chain package
    if size := tx.Size(); size > MaxTransactionSize {
        return fmt.Errorf("transaction too large: %d > %d", size, MaxTransactionSize)
    }

    // Validate nonce
    if err := t.validateNonce(tx); err != nil {
        return err
    }

    // Validate balance
    if err := t.validateBalance(tx); err != nil {
        return err
    }

    return nil
}

func (t *TransactionSubmitter) validateNonce(tx *chain.Transaction) error {
    // Implementation would check nonce against account state
    return nil
}

func (t *TransactionSubmitter) validateBalance(tx *chain.Transaction) error {
    // Implementation would check balance against account state
    return nil
}

func (t *TransactionSubmitter) signTransaction(tx *chain.Transaction) (*chain.Transaction, error) {
    return t.signer.SignTransaction(tx)
}

func (t *TransactionSubmitter) updateMetrics(tx *chain.Transaction) {
    t.metrics.lock.Lock()
    defer t.metrics.lock.Unlock()

    t.metrics.Submitted++
    t.metrics.TotalFees += calculateTransactionFee(tx)
    t.metrics.AvgGasPrice = t.metrics.TotalFees / t.metrics.Submitted
}

// GetPendingTxs returns currently pending transactions
func (t *TransactionSubmitter) GetPendingTxs() []*chain.Transaction {
    t.lock.RLock()
    defer t.lock.RUnlock()

    txs := make([]*chain.Transaction, 0, len(t.pending))
    for _, pending := range t.pending {
        txs = append(txs, pending.Tx)
    }
    return txs
}

// GetMetrics returns current transaction metrics
func (t *TransactionSubmitter) GetMetrics() TxMetrics {
    t.metrics.lock.Lock()
    defer t.metrics.lock.Unlock()
    return *t.metrics
}

func isTransactionSigned(tx *chain.Transaction) bool {
    return len(tx.Signature) > 0  // Adjust based on actual chain.Transaction structure
}

func calculateTransactionFee(tx *chain.Transaction) uint64 {
    return tx.GasPrice * tx.GasLimit  // Adjust based on actual chain.Transaction structure
}

// TxPool defines the interface for transaction pool interaction
type TxPool interface {
    AddLocal(*chain.Transaction) error
    AddLocalBatch([]*chain.Transaction) error
}

// Helper function to create a batch transaction
func CreateBatchTransaction(txs []*chain.Transaction) (*chain.Transaction, error) {
    // Implementation would combine multiple transactions into a single batch transaction
    return nil, nil
}

// Helper function to estimate transaction gas
func EstimateGas(tx *chain.Transaction) (uint64, error) {
    // Implementation would estimate gas needed for transaction
    return 0, nil
}