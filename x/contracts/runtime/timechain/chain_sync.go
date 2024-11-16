// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timechain

import (
    "context"
    "errors"
    "fmt"
    "sync"
    "time"

    "github.com/ava-labs/avalanchego/ids"
)

var (
    ErrBlockHeightMismatch = errors.New("block height mismatch")
    ErrChainReorg         = errors.New("chain reorganization detected")
    ErrRollbackFailed     = errors.New("state rollback failed")
    ErrInvalidBlockHeight = errors.New("invalid block height")
)

// ChainState represents the blockchain state
type ChainState struct {
    Height    uint64
    BlockID   ids.ID
    Timestamp time.Time
}

// BlockSync handles chain synchronization
type BlockSync struct {
    sequence *TimeSequence
    
    // Current chain state
    currentState ChainState
    lastState    ChainState
    
    // Block height mapping
    heightMap    map[uint64]*WindowState
    
    // Rollback support
    rollbackPoints map[uint64]*RollbackPoint
    
    mutex         sync.RWMutex
}

type WindowState struct {
    Window     *TimeWindow
    BlockStart uint64
    BlockEnd   uint64
    BlockHash  ids.ID
}

type RollbackPoint struct {
    State      ChainState
    Windows    []*TimeWindow
    LastEntry  *SequenceEntry
    NextSeqNum uint64
}

func NewBlockSync(sequence *TimeSequence) *BlockSync {
    return &BlockSync{
        sequence:      sequence,
        heightMap:     make(map[uint64]*WindowState),
        rollbackPoints: make(map[uint64]*RollbackPoint),
    }
}

// OnBlockCommit handles new block commits
func (bs *BlockSync) OnBlockCommit(height uint64, blockID ids.ID, timestamp time.Time) error {
    bs.mutex.Lock()
    defer bs.mutex.Unlock()

    // Validate block height
    if height <= bs.currentState.Height {
        return fmt.Errorf("%w: current %d, got %d", 
            ErrInvalidBlockHeight, bs.currentState.Height, height)
    }

    // Update state
    bs.lastState = bs.currentState
    bs.currentState = ChainState{
        Height:    height,
        BlockID:   blockID,
        Timestamp: timestamp,
    }

    // Create rollback point
    if err := bs.createRollbackPoint(height); err != nil {
        return fmt.Errorf("failed to create rollback point: %w", err)
    }

    // Update window mapping
    if err := bs.updateWindowMapping(height, blockID); err != nil {
        return fmt.Errorf("failed to update window mapping: %w", err)
    }

    return nil
}

// OnBlockReorg handles chain reorganizations
func (bs *BlockSync) OnBlockReorg(height uint64) error {
    bs.mutex.Lock()
    defer bs.mutex.Unlock()

    // Find rollback point
    rollback, exists := bs.rollbackPoints[height]
    if !exists {
        return fmt.Errorf("%w: no rollback point at height %d", 
            ErrRollbackFailed, height)
    }

    // Perform rollback
    if err := bs.rollbackTo(rollback); err != nil {
        return fmt.Errorf("rollback failed: %w", err)
    }

    // Clean up rollback points after height
    for h := range bs.rollbackPoints {
        if h > height {
            delete(bs.rollbackPoints, h)
        }
    }

    // Clean up height mappings
    for h := range bs.heightMap {
        if h > height {
            delete(bs.heightMap, h)
        }
    }

    return nil
}

// createRollbackPoint creates a new rollback point
func (bs *BlockSync) createRollbackPoint(height uint64) error {
    point := &RollbackPoint{
        State:      bs.currentState,
        Windows:    make([]*TimeWindow, len(bs.sequence.windows)),
        LastEntry:  bs.sequence.lastEntry,
        NextSeqNum: bs.sequence.nextSeqNum,
    }

    // Deep copy windows
    for i, window := range bs.sequence.windows {
        copiedWindow := &TimeWindow{
            StartTime:       window.StartTime,
            EndTime:         window.EndTime,
            Entries:         make([]*SequenceEntry, len(window.Entries)),
            RoughtimeProofs: make([]*RoughtimeProof, len(window.RoughtimeProofs)),
            PrevWindowHash:  window.PrevWindowHash,
        }
        copy(copiedWindow.Entries, window.Entries)
        copy(copiedWindow.RoughtimeProofs, window.RoughtimeProofs)
        point.Windows[i] = copiedWindow
    }

    bs.rollbackPoints[height] = point
    return nil
}

// rollbackTo restores state to a rollback point
func (bs *BlockSync) rollbackTo(point *RollbackPoint) error {
    // Restore sequence state
    bs.sequence.mutex.Lock()
    defer bs.sequence.mutex.Unlock()

    bs.sequence.windows = point.Windows
    bs.sequence.lastEntry = point.LastEntry
    bs.sequence.nextSeqNum = point.NextSeqNum

    if len(point.Windows) > 0 {
        bs.sequence.currentWindow = point.Windows[len(point.Windows)-1]
    } else {
        bs.sequence.currentWindow = nil
    }

    // Restore chain state
    bs.currentState = point.State

    return nil
}

// updateWindowMapping updates the block height to window mapping
func (bs *BlockSync) updateWindowMapping(height uint64, blockID ids.ID) error {
    currentWindow := bs.sequence.currentWindow
    if currentWindow == nil {
        return nil
    }

    // Find or create window state
    state, exists := bs.heightMap[height]
    if !exists {
        state = &WindowState{
            Window:     currentWindow,
            BlockStart: height,
            BlockHash:  blockID,
        }
        bs.heightMap[height] = state
    }

    // Update end height for previous window if exists
    for h, s := range bs.heightMap {
        if s.Window == currentWindow && h != height {
            s.BlockEnd = height - 1
        }
    }

    return nil
}

// GetWindowForBlock returns the time window containing a block height
func (bs *BlockSync) GetWindowForBlock(height uint64) (*TimeWindow, error) {
    bs.mutex.RLock()
    defer bs.mutex.RUnlock()

    state, exists := bs.heightMap[height]
    if !exists {
        return nil, fmt.Errorf("no window found for height %d", height)
    }

    return state.Window, nil
}

// GetBlockRange returns the block height range for a window
func (bs *BlockSync) GetBlockRange(window *TimeWindow) (uint64, uint64, error) {
    bs.mutex.RLock()
    defer bs.mutex.RUnlock()

    for _, state := range bs.heightMap {
        if state.Window == window {
            return state.BlockStart, state.BlockEnd, nil
        }
    }

    return 0, 0, fmt.Errorf("window not found in block mapping")
}

// CleanupRollbackPoints removes old rollback points
func (bs *BlockSync) CleanupRollbackPoints(oldestHeight uint64) {
    bs.mutex.Lock()
    defer bs.mutex.Unlock()

    for height := range bs.rollbackPoints {
        if height < oldestHeight {
            delete(bs.rollbackPoints, height)
        }
    }
}