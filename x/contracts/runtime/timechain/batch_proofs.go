// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timechain

import (
    "errors"
    "fmt"
    "sync"
    "time"

    "golang.org/x/crypto/sha3"
)

var (
    ErrBatchTooLarge      = errors.New("proof batch size exceeds maximum")
    ErrInvalidBatchRange  = errors.New("invalid batch sequence range")
    ErrBatchGenFailed     = errors.New("batch proof generation failed")
    ErrIncompleteProofs   = errors.New("could not generate all requested proofs")
)

// BatchProofRequest defines parameters for batch proof generation
type BatchProofRequest struct {
    // Sequence numbers to generate proofs for
    SequenceNums []uint64
    
    // Optional time range to filter entries
    StartTime *time.Time
    EndTime   *time.Time
    
    // Optional block height constraint
    BlockHeight *uint64
    
    // Maximum number of proofs to generate
    MaxBatchSize uint64
    
    // Whether to allow partial batch completion
    AllowPartial bool
}

// BatchProofResponse contains the generated proofs
type BatchProofResponse struct {
    // Generated proofs
    Proofs []*SequenceProof
    
    // Batch metadata
    BatchRoot []byte
    BatchSize uint64
    
    // Time range of included proofs
    TimeRange    [2]time.Time
    
    // Statistics
    RequestedCount uint64
    GeneratedCount uint64
    FailedCount   uint64
}

// ProofBatchGenerator handles batch proof generation
type ProofBatchGenerator struct {
    sequence *TimeSequence
    
    // Concurrent generation control
    workerCount int
    workChan    chan proofWorkItem
    resultChan  chan proofWorkResult
    
    // Batch assembly
    mutex       sync.Mutex
    batches     map[string]*BatchProofResponse
}

type proofWorkItem struct {
    entry     *SequenceEntry
    window    *TimeWindow
    batchID   string
}

type proofWorkResult struct {
    proof     *SequenceProof
    err       error
    batchID   string
    seqNum    uint64
}

// NewProofBatchGenerator creates a new batch generator
func NewProofBatchGenerator(sequence *TimeSequence, workerCount int) *ProofBatchGenerator {
    if workerCount <= 0 {
        workerCount = 4 // Default worker count
    }
    
    return &ProofBatchGenerator{
        sequence:    sequence,
        workerCount: workerCount,
        workChan:    make(chan proofWorkItem, workerCount*2),
        resultChan:  make(chan proofWorkResult, workerCount*2),
        batches:     make(map[string]*BatchProofResponse),
    }
}

// GenerateProofBatch generates a batch of proofs
func (pbg *ProofBatchGenerator) GenerateProofBatch(request *BatchProofRequest) (*BatchProofResponse, error) {
    if err := pbg.validateRequest(request); err != nil {
        return nil, err
    }

    // Generate batch ID
    batchID := pbg.generateBatchID(request)
    
    // Initialize batch response
    response := &BatchProofResponse{
        Proofs:         make([]*SequenceProof, 0, len(request.SequenceNums)),
        RequestedCount: uint64(len(request.SequenceNums)),
    }
    pbg.batches[batchID] = response

    // Start worker pool
    pbg.startWorkers()
    defer pbg.stopWorkers()

    // Submit work items
    if err := pbg.submitBatchWork(batchID, request); err != nil {
        return nil, err
    }

    // Collect results
    if err := pbg.collectBatchResults(batchID, response, request.AllowPartial); err != nil {
        return nil, err
    }

    // Generate batch root
    if err := pbg.generateBatchRoot(response); err != nil {
        return nil, err
    }

    return response, nil
}

func (pbg *ProofBatchGenerator) validateRequest(request *BatchProofRequest) error {
    if len(request.SequenceNums) == 0 {
        return errors.New("empty sequence number list")
    }
    
    if uint64(len(request.SequenceNums)) > request.MaxBatchSize {
        return fmt.Errorf("%w: requested %d, max %d", 
            ErrBatchTooLarge, len(request.SequenceNums), request.MaxBatchSize)
    }

    if request.StartTime != nil && request.EndTime != nil {
        if request.EndTime.Before(*request.StartTime) {
            return ErrInvalidBatchRange
        }
    }

    return nil
}

func (pbg *ProofBatchGenerator) startWorkers() {
    for i := 0; i < pbg.workerCount; i++ {
        go pbg.proofWorker()
    }
}

func (pbg *ProofBatchGenerator) stopWorkers() {
    close(pbg.workChan)
}

func (pbg *ProofBatchGenerator) proofWorker() {
    for work := range pbg.workChan {
        proof, err := pbg.generateProof(work.entry, work.window)
        pbg.resultChan <- proofWorkResult{
            proof:   proof,
            err:     err,
            batchID: work.batchID,
            seqNum:  work.entry.SequenceNum,
        }
    }
}

func (pbg *ProofBatchGenerator) generateProof(entry *SequenceEntry, window *TimeWindow) (*SequenceProof, error) {
    proofPath, err := generateProofPath(window, entry)
    if err != nil {
        return nil, err
    }

    return &SequenceProof{
        Entry:        entry,
        WindowRoot:   window.GetRoot(),
        WindowBounds: [2]time.Time{window.StartTime, window.EndTime},
        ProofPath:    proofPath,
    }, nil
}

func (pbg *ProofBatchGenerator) submitBatchWork(batchID string, request *BatchProofRequest) error {
    pbg.sequence.mutex.RLock()
    defer pbg.sequence.mutex.RUnlock()

    for _, seqNum := range request.SequenceNums {
        entry := pbg.sequence.GetSequenceEntry(seqNum)
        if entry == nil {
            continue
        }

        // Apply time range filter if specified
        if request.StartTime != nil && entry.VerifiedTime.Before(*request.StartTime) {
            continue
        }
        if request.EndTime != nil && entry.VerifiedTime.After(*request.EndTime) {
            continue
        }

        // Find containing window
        window := pbg.findContainingWindow(entry)
        if window == nil {
            continue
        }

        // Apply block height filter if specified
        if request.BlockHeight != nil {
            blockStart, _, err := pbg.sequence.blockSync.GetBlockRange(window)
            if err != nil || blockStart > *request.BlockHeight {
                continue
            }
        }

        pbg.workChan <- proofWorkItem{
            entry:   entry,
            window:  window,
            batchID: batchID,
        }
    }

    return nil
}

func (pbg *ProofBatchGenerator) collectBatchResults(
    batchID string, 
    response *BatchProofResponse, 
    allowPartial bool,
) error {
    remaining := response.RequestedCount
    
    for remaining > 0 {
        result := <-pbg.resultChan
        if result.batchID != batchID {
            continue
        }

        remaining--

        if result.err != nil {
            response.FailedCount++
            if !allowPartial {
                return fmt.Errorf("%w: %v", ErrBatchGenFailed, result.err)
            }
            continue
        }

        response.Proofs = append(response.Proofs, result.proof)
        response.GeneratedCount++

        // Update time range
        if len(response.Proofs) == 1 {
            response.TimeRange[0] = result.proof.Entry.VerifiedTime
            response.TimeRange[1] = result.proof.Entry.VerifiedTime
        } else {
            if result.proof.Entry.VerifiedTime.Before(response.TimeRange[0]) {
                response.TimeRange[0] = result.proof.Entry.VerifiedTime
            }
            if result.proof.Entry.VerifiedTime.After(response.TimeRange[1]) {
                response.TimeRange[1] = result.proof.Entry.VerifiedTime
            }
        }
    }

    if !allowPartial && response.GeneratedCount != response.RequestedCount {
        return ErrIncompleteProofs
    }

    return nil
}

func (pbg *ProofBatchGenerator) generateBatchRoot(response *BatchProofResponse) error {
    hasher := sha3.New256()
    
    // Hash all proofs in order
    for _, proof := range response.Proofs {
        proofHash := proof.GetProofHash()
        hasher.Write(proofHash)
    }
    
    response.BatchRoot = hasher.Sum(nil)
    response.BatchSize = uint64(len(response.Proofs))
    
    return nil
}

func (pbg *ProofBatchGenerator) findContainingWindow(entry *SequenceEntry) *TimeWindow {
    for _, window := range pbg.sequence.windows {
        if containsEntry(window, entry) {
            return window
        }
    }
    return nil
}

func (pbg *ProofBatchGenerator) generateBatchID(request *BatchProofRequest) string {
    hasher := sha3.New256()
    
    // Hash sequence numbers
    for _, seqNum := range request.SequenceNums {
        binary.Write(hasher, binary.BigEndian, seqNum)
    }
    
    // Hash time range if specified
    if request.StartTime != nil {
        binary.Write(hasher, binary.BigEndian, request.StartTime.UnixNano())
    }
    if request.EndTime != nil {
        binary.Write(hasher, binary.BigEndian, request.EndTime.UnixNano())
    }
    
    // Hash block height if specified
    if request.BlockHeight != nil {
        binary.Write(hasher, binary.BigEndian, *request.BlockHeight)
    }
    
    return string(hasher.Sum(nil))
}