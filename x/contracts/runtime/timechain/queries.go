// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timechain

import (
    "errors"
    "fmt"
    "time"

    "github.com/ava-labs/avalanchego/ids"
)

var (
    ErrInvalidTimeRange    = errors.New("invalid time range")
    ErrNoWindowsInRange    = errors.New("no windows in specified range")
    ErrBatchSizeTooLarge   = errors.New("batch size too large")
    ErrWindowNotFound      = errors.New("window not found")
    ErrProofGenerationFailed = errors.New("proof generation failed")
)

// QueryResult represents a time range query result
type QueryResult struct {
    Windows   []*TimeWindow
    Entries   []*SequenceEntry
    StartTime time.Time
    EndTime   time.Time
    Proofs    []*SequenceProof
}

// BatchProofRequest represents a request for multiple proofs
type BatchProofRequest struct {
    SequenceNums []uint64
    BlockHeight  uint64
    MaxBatchSize uint64
}

// HistoricalQuery represents a query for historical windows
type HistoricalQuery struct {
    StartTime   time.Time
    EndTime     time.Time
    BlockHeight uint64
    IncludeProofs bool
}

// Add these methods to TimeSequence

// QueryTimeRange returns entries within the specified time range
func (ts *TimeSequence) QueryTimeRange(start, end time.Time) (*QueryResult, error) {
    ts.mutex.RLock()
    defer ts.mutex.RUnlock()

    if end.Before(start) {
        return nil, ErrInvalidTimeRange
    }

    result := &QueryResult{
        StartTime: start,
        EndTime:   end,
        Windows:   make([]*TimeWindow, 0),
        Entries:   make([]*SequenceEntry, 0),
        Proofs:    make([]*SequenceProof, 0),
    }

    // Find all windows that overlap with the range
    for _, window := range ts.windows {
        if window.EndTime.Before(start) || window.StartTime.After(end) {
            continue
        }

        result.Windows = append(result.Windows, window)

        // Add entries from this window that fall within range
        for _, entry := range window.Entries {
            if entry.VerifiedTime.After(start) && entry.VerifiedTime.Before(end) {
                result.Entries = append(result.Entries, entry)

                // Generate proof for each entry
                proof, err := ts.generateProof(entry)
                if err != nil {
                    return nil, fmt.Errorf("failed to generate proof: %w", err)
                }
                result.Proofs = append(result.Proofs, proof)
            }
        }
    }

    if len(result.Windows) == 0 {
        return nil, ErrNoWindowsInRange
    }

    return result, nil
}

// GenerateProofBatch generates proofs for multiple sequence numbers
func (ts *TimeSequence) GenerateProofBatch(request *BatchProofRequest) ([]*SequenceProof, error) {
    ts.mutex.RLock()
    defer ts.mutex.RUnlock()

    if uint64(len(request.SequenceNums)) > request.MaxBatchSize {
        return nil, ErrBatchSizeTooLarge
    }

    proofs := make([]*SequenceProof, 0, len(request.SequenceNums))

    for _, seqNum := range request.SequenceNums {
        // Find entry
        entry := ts.GetSequenceEntry(seqNum)
        if entry == nil {
            continue
        }

        // Generate proof
        proof, err := ts.generateProof(entry)
        if err != nil {
            return nil, fmt.Errorf("failed to generate proof for sequence %d: %w", seqNum, err)
        }

        proofs = append(proofs, proof)
    }

    return proofs, nil
}

// QueryHistoricalWindows retrieves historical windows with optional proofs
func (ts *TimeSequence) QueryHistoricalWindows(query *HistoricalQuery) (*QueryResult, error) {
    ts.mutex.RLock()
    defer ts.mutex.RUnlock()

    if query.EndTime.Before(query.StartTime) {
        return nil, ErrInvalidTimeRange
    }

    result := &QueryResult{
        StartTime: query.StartTime,
        EndTime:   query.EndTime,
        Windows:   make([]*TimeWindow, 0),
        Entries:   make([]*SequenceEntry, 0),
    }

    // Find windows in range
    for _, window := range ts.windows {
        if window.EndTime.Before(query.StartTime) || window.StartTime.After(query.EndTime) {
            continue
        }

        // Verify window is at or before requested block height
        blockStart, _, err := ts.blockSync.GetBlockRange(window)
        if err != nil {
            continue
        }
        if blockStart > query.BlockHeight {
            continue
        }

        result.Windows = append(result.Windows, window)
        result.Entries = append(result.Entries, window.Entries...)

        // Generate proofs if requested
        if query.IncludeProofs {
            for _, entry := range window.Entries {
                proof, err := ts.generateProof(entry)
                if err != nil {
                    return nil, fmt.Errorf("failed to generate proof: %w", err)
                }
                result.Proofs = append(result.Proofs, proof)
            }
        }
    }

    if len(result.Windows) == 0 {
        return nil, ErrNoWindowsInRange
    }

    return result, nil
}

// Helper method to generate a proof for an entry
func (ts *TimeSequence) generateProof(entry *SequenceEntry) (*SequenceProof, error) {
    // Find containing window
    var window *TimeWindow
    for _, w := range ts.windows {
        if containsEntry(w, entry) {
            window = w
            break
        }
    }

    if window == nil {
        return nil, ErrWindowNotFound
    }

    // Generate proof path
    proofPath, err := generateProofPath(window, entry)
    if err != nil {
        return nil, fmt.Errorf("%w: %v", ErrProofGenerationFailed, err)
    }

    return &SequenceProof{
        Entry:         entry,
        WindowRoot:    window.GetRoot(),
        WindowBounds:  [2]time.Time{window.StartTime, window.EndTime},
        ProofPath:    proofPath,
    }, nil
}

// Add these helper methods for querying

// GetEntriesByBlockHeight returns entries at a specific block height
func (ts *TimeSequence) GetEntriesByBlockHeight(height uint64) ([]*SequenceEntry, error) {
    window, err := ts.blockSync.GetWindowForBlock(height)
    if err != nil {
        return nil, err
    }

    entries := make([]*SequenceEntry, 0)
    for _, entry := range window.Entries {
        // Only include entries that were added at or before this height
        blockStart, blockEnd, err := ts.blockSync.GetBlockRange(window)
        if err != nil {
            continue
        }
        if height >= blockStart && height <= blockEnd {
            entries = append(entries, entry)
        }
    }

    return entries, nil
}

// GetWindowsByBlockRange returns windows within a block height range
func (ts *TimeSequence) GetWindowsByBlockRange(startHeight, endHeight uint64) ([]*TimeWindow, error) {
    if endHeight < startHeight {
        return nil, ErrInvalidTimeRange
    }

    windows := make([]*TimeWindow, 0)
    for _, window := range ts.windows {
        blockStart, blockEnd, err := ts.blockSync.GetBlockRange(window)
        if err != nil {
            continue
        }

        // Include window if it overlaps with the block range
        if blockEnd >= startHeight && blockStart <= endHeight {
            windows = append(windows, window)
        }
    }

    return windows, nil
}

// GetLatestProof returns the proof for the most recent entry
func (ts *TimeSequence) GetLatestProof() (*SequenceProof, error) {
    if ts.lastEntry == nil {
        return nil, errors.New("no entries in sequence")
    }

    return ts.generateProof(ts.lastEntry)
}