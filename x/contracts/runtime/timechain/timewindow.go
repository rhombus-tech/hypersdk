// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timechain

import (
    "crypto/sha256"
    "errors"
    "fmt"
    "sync"
    "time"

    "golang.org/x/crypto/sha3"
)

var (
    ErrEntryOutOfOrder  = errors.New("entry out of temporal order")
    ErrWindowClosed     = errors.New("window is closed")
    ErrDuplicateEntry   = errors.New("duplicate entry in window")
    ErrInvalidHashChain = errors.New("invalid hash chain linkage")
)

// TimeWindow represents a fixed time interval containing sequence entries
type TimeWindow struct {
    StartTime       time.Time
    EndTime         time.Time
    Entries         []*SequenceEntry
    RoughtimeProofs []*RoughtimeProof
    
    // Merkle tree state
    root            []byte
    nodes           [][]byte
    
    // Window state
    isClosed        bool
    maxEntries      uint64
    
    // Synchronization
    mutex           sync.RWMutex
}

// NewTimeWindow creates a new time window
func NewTimeWindow(start time.Time, duration time.Duration, maxEntries uint64) *TimeWindow {
    return &TimeWindow{
        StartTime:       start,
        EndTime:         start.Add(duration),
        Entries:         make([]*SequenceEntry, 0),
        RoughtimeProofs: make([]*RoughtimeProofs, 0),
        nodes:           make([][]byte, 0),
        maxEntries:      maxEntries,
    }
}

// AddEntry adds a new entry to the window
func (w *TimeWindow) AddEntry(entry *SequenceEntry) error {
    w.mutex.Lock()
    defer w.mutex.Unlock()

    if w.isClosed {
        return ErrWindowClosed
    }

    // Validate entry
    if err := w.validateEntry(entry); err != nil {
        return err
    }

    // Add entry
    w.Entries = append(w.Entries, entry)
    w.RoughtimeProofs = append(w.RoughtimeProofs, entry.TimeProof)

    // Update Merkle tree
    if err := w.updateMerkleTree(entry); err != nil {
        // Rollback on error
        w.Entries = w.Entries[:len(w.Entries)-1]
        w.RoughtimeProofs = w.RoughtimeProofs[:len(w.RoughtimeProofs)-1]
        return fmt.Errorf("failed to update merkle tree: %w", err)
    }

    // Check if window should be closed
    if uint64(len(w.Entries)) >= w.maxEntries {
        w.close()
    }

    return nil
}

// validateEntry performs entry validation
func (w *TimeWindow) validateEntry(entry *SequenceEntry) error {
    // Check time bounds
    if entry.VerifiedTime.Before(w.StartTime) || entry.VerifiedTime.After(w.EndTime) {
        return ErrEntryOutOfOrder
    }

    // Check for duplicates
    for _, e := range w.Entries {
        if e.SequenceNum == entry.SequenceNum {
            return ErrDuplicateEntry
        }
    }

    // Verify temporal ordering
    if len(w.Entries) > 0 {
        lastEntry := w.Entries[len(w.Entries)-1]
        if !entry.VerifiedTime.After(lastEntry.VerifiedTime) {
            return ErrEntryOutOfOrder
        }
    }

    return nil
}

// updateMerkleTree updates the Merkle tree with a new entry
func (w *TimeWindow) updateMerkleTree(entry *SequenceEntry) error {
    // Hash the entry
    entryHash := calculateEntryHash(entry)
    w.nodes = append(w.nodes, entryHash)

    // Rebuild tree if we have more than one node
    if len(w.nodes) > 1 {
        if err := w.rebuildMerkleTree(); err != nil {
            return err
        }
    } else {
        w.root = entryHash
    }

    return nil
}

// rebuildMerkleTree rebuilds the entire Merkle tree
func (w *TimeWindow) rebuildMerkleTree() error {
    if len(w.nodes) == 0 {
        return errors.New("no nodes to build tree")
    }

    currentLevel := w.nodes
    for len(currentLevel) > 1 {
        nextLevel := make([][]byte, 0, (len(currentLevel)+1)/2)

        for i := 0; i < len(currentLevel); i += 2 {
            // Get left node
            left := currentLevel[i]

            // Get right node (may be nil if odd number of nodes)
            var right []byte
            if i+1 < len(currentLevel) {
                right = currentLevel[i+1]
            } else {
                right = left // Duplicate last node if odd
            }

            // Combine nodes
            parent := w.hashNodes(left, right)
            nextLevel = append(nextLevel, parent)
        }

        currentLevel = nextLevel
    }

    w.root = currentLevel[0]
    return nil
}

// hashNodes combines two node hashes
func (w *TimeWindow) hashNodes(left, right []byte) []byte {
    hasher := sha3.New256()
    hasher.Write(left)
    hasher.Write(right)
    return hasher.Sum(nil)
}

// GetRoot returns the Merkle root hash
func (w *TimeWindow) GetRoot() []byte {
    w.mutex.RLock()
    defer w.mutex.RUnlock()
    return w.root
}

// GenerateProof generates a Merkle proof for an entry
func (w *TimeWindow) GenerateProof(sequenceNum uint64) (*MerkleProof, error) {
    w.mutex.RLock()
    defer w.mutex.RUnlock()

    // Find entry index
    index := -1
    for i, entry := range w.Entries {
        if entry.SequenceNum == sequenceNum {
            index = i
            break
        }
    }

    if index == -1 {
        return nil, errors.New("entry not found")
    }

    // Generate proof path
    path := make([][]byte, 0)
    currentIndex := index
    currentLevel := w.nodes

    for len(currentLevel) > 1 {
        // Get sibling index
        siblingIndex := currentIndex
        if currentIndex%2 == 0 {
            siblingIndex++
        } else {
            siblingIndex--
        }

        // Add sibling to proof path if it exists
        if siblingIndex < len(currentLevel) {
            path = append(path, currentLevel[siblingIndex])
        }

        // Move to parent level
        currentIndex /= 2
        currentLevel = w.getParentLevel(currentLevel)
    }

    return &MerkleProof{
        Root:      w.root,
        Path:      path,
        Index:     uint64(index),
        LeafHash:  w.nodes[index],
    }, nil
}

// getParentLevel calculates parent level nodes
func (w *TimeWindow) getParentLevel(level [][]byte) [][]byte {
    parentLevel := make([][]byte, 0, (len(level)+1)/2)
    for i := 0; i < len(level); i += 2 {
        left := level[i]
        var right []byte
        if i+1 < len(level) {
            right = level[i+1]
        } else {
            right = left
        }
        parent := w.hashNodes(left, right)
        parentLevel = append(parentLevel, parent)
    }
    return parentLevel
}

// close finalizes the window
func (w *TimeWindow) close() {
    w.isClosed = true
}

// IsClosed returns window state
func (w *TimeWindow) IsClosed() bool {
    w.mutex.RLock()
    defer w.mutex.RUnlock()
    return w.isClosed
}

// GetEntries returns window entries
func (w *TimeWindow) GetEntries() []*SequenceEntry {
    w.mutex.RLock()
    defer w.mutex.RUnlock()
    entries := make([]*SequenceEntry, len(w.Entries))
    copy(entries, w.Entries)
    return entries
}

// GetEntryCount returns number of entries
func (w *TimeWindow) GetEntryCount() int {
    w.mutex.RLock()
    defer w.mutex.RUnlock()
    return len(w.Entries)
}

// ContainsTime checks if time falls within window
func (w *TimeWindow) ContainsTime(t time.Time) bool {
    return !t.Before(w.StartTime) && !t.After(w.EndTime)
}

// GetTimeRange returns window time bounds
func (w *TimeWindow) GetTimeRange() (time.Time, time.Time) {
    return w.StartTime, w.EndTime
}

// generateProofPath creates a Merkle proof path for a specific entry
func (w *TimeWindow) generateProofPath(entry *SequenceEntry) ([][]byte, error) {
    w.mutex.RLock()
    defer w.mutex.RUnlock()

    // Find entry index
    entryIndex := -1
    for i, e := range w.Entries {
        if e.SequenceNum == entry.SequenceNum {
            entryIndex = i
            break
        }
    }

    if entryIndex == -1 {
        return nil, errors.New("entry not found in window")
    }

    // Create proof path
    proofPath := make([][]byte, 0)
    currentLevel := w.nodes[0] // Leaf level
    currentIndex := entryIndex

    for len(currentLevel) > 1 {
        // Determine sibling position
        siblingIndex := currentIndex
        if currentIndex%2 == 0 {
            // Current node is left child, sibling is right
            siblingIndex++
        } else {
            // Current node is right child, sibling is left
            siblingIndex--
        }

        // Add sibling to proof if it exists
        if siblingIndex < len(currentLevel) {
            proofPath = append(proofPath, currentLevel[siblingIndex])
        } else {
            // Use the current node as its own sibling if at end of odd-length level
            proofPath = append(proofPath, currentLevel[currentIndex])
        }

        // Move to parent level
        currentIndex /= 2
        currentLevel = w.getParentLevel(currentLevel)
    }

    return proofPath, nil
}

// getParentLevel calculates nodes for the next level up in the tree
func (w *TimeWindow) getParentLevel(level [][]byte) [][]byte {
    parentLevel := make([][]byte, 0, (len(level)+1)/2)

    for i := 0; i < len(level); i += 2 {
        left := level[i]
        var right []byte

        if i+1 < len(level) {
            right = level[i+1]
        } else {
            // Duplicate last node if odd number of nodes
            right = left
        }

        parent := w.hashNodes(left, right)
        parentLevel = append(parentLevel, parent)
    }

    return parentLevel
}

// hashNodes combines two node hashes
func (w *TimeWindow) hashNodes(left, right []byte) []byte {
    hasher := sha3.New256()
    hasher.Write(left)
    hasher.Write(right)
    return hasher.Sum(nil)
}

// verifyProofPath verifies a proof path for an entry
func (w *TimeWindow) verifyProofPath(entry *SequenceEntry, proofPath [][]byte) error {
    entryHash := calculateEntryHash(entry)
    currentHash := entryHash

    for _, sibling := range proofPath {
        hasher := sha3.New256()
        
        // Order doesn't matter as long as it's consistent
        hasher.Write(currentHash)
        hasher.Write(sibling)
        
        currentHash = hasher.Sum(nil)
    }

    // Final hash should match window root
    if !bytes.Equal(currentHash, w.root) {
        return errors.New("invalid proof path")
    }

    return nil
}