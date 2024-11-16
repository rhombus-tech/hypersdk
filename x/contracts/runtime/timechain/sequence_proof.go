// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timechain

import (
    "errors"
    "fmt"
    "time"

    "golang.org/x/crypto/sha3"
)

var (
    ErrInvalidSequenceProof = errors.New("invalid sequence proof")
    ErrProofOutOfWindow     = errors.New("proof outside time window")
    ErrInvalidWindowBounds  = errors.New("invalid window boundaries")
    ErrMissingProofElement  = errors.New("missing proof element")
)

// SequenceProof represents a proof of an entry in the time sequence
type SequenceProof struct {
    // The sequence entry being proven
    Entry *SequenceEntry

    // The Merkle root of the containing window
    WindowRoot []byte

    // The time boundaries of the window [start, end]
    WindowBounds [2]time.Time

    // The Merkle proof path for the entry
    ProofPath [][]byte

    // Additional metadata
    Metadata struct {
        // Index of entry in window
        WindowIndex uint64

        // Total entries in window at time of proof
        WindowSize uint64

        // Hash of previous window's last entry (if any)
        PrevWindowLastHash []byte
    }
}

// NewSequenceProof creates a new sequence proof
func NewSequenceProof(
    entry *SequenceEntry,
    windowRoot []byte,
    bounds [2]time.Time,
    proofPath [][]byte,
) *SequenceProof {
    return &SequenceProof{
        Entry:        entry,
        WindowRoot:   windowRoot,
        WindowBounds: bounds,
        ProofPath:    proofPath,
    }
}

// Verify verifies the sequence proof
func (sp *SequenceProof) Verify(verifier *Verifier) error {
    if err := sp.validateFields(); err != nil {
        return fmt.Errorf("%w: %v", ErrInvalidSequenceProof, err)
    }

    // Verify the entry is within window bounds
    if err := sp.verifyTimeWindow(); err != nil {
        return err
    }

    // Verify the entry's time proof
    if err := verifier.VerifyTimeProof(sp.Entry.TimeProof); err != nil {
        return fmt.Errorf("time proof verification failed: %w", err)
    }

    // Verify the Merkle proof
    merkleProof := &MerkleProof{
        Root:     sp.WindowRoot,
        Path:     sp.ProofPath,
        Index:    sp.Metadata.WindowIndex,
        LeafHash: calculateEntryHash(sp.Entry),
    }

    if err := merkleProof.Verify(); err != nil {
        return fmt.Errorf("merkle proof verification failed: %w", err)
    }

    return nil
}

// validateFields checks if all required fields are present
func (sp *SequenceProof) validateFields() error {
    if sp.Entry == nil {
        return errors.New("missing entry")
    }
    if len(sp.WindowRoot) == 0 {
        return errors.New("missing window root")
    }
    if sp.WindowBounds[0].IsZero() || sp.WindowBounds[1].IsZero() {
        return ErrInvalidWindowBounds
    }
    if len(sp.ProofPath) == 0 {
        return errors.New("missing proof path")
    }
    return nil
}

// verifyTimeWindow verifies the entry is within the window bounds
func (sp *SequenceProof) verifyTimeWindow() error {
    entryTime := sp.Entry.VerifiedTime
    if entryTime.Before(sp.WindowBounds[0]) || entryTime.After(sp.WindowBounds[1]) {
        return ErrProofOutOfWindow
    }
    return nil
}

// GetProofHash generates a unique hash for the proof
func (sp *SequenceProof) GetProofHash() []byte {
    hasher := sha3.New256()

    // Hash entry
    entryHash := calculateEntryHash(sp.Entry)
    hasher.Write(entryHash)

    // Hash window root
    hasher.Write(sp.WindowRoot)

    // Hash window bounds
    timeBytes := make([]byte, 16)
    binary.BigEndian.PutUint64(timeBytes[:8], uint64(sp.WindowBounds[0].UnixNano()))
    binary.BigEndian.PutUint64(timeBytes[8:], uint64(sp.WindowBounds[1].UnixNano()))
    hasher.Write(timeBytes)

    // Hash proof path
    for _, path := range sp.ProofPath {
        hasher.Write(path)
    }

    // Hash metadata
    metaBytes := make([]byte, 16)
    binary.BigEndian.PutUint64(metaBytes[:8], sp.Metadata.WindowIndex)
    binary.BigEndian.PutUint64(metaBytes[8:], sp.Metadata.WindowSize)
    hasher.Write(metaBytes)

    if len(sp.Metadata.PrevWindowLastHash) > 0 {
        hasher.Write(sp.Metadata.PrevWindowLastHash)
    }

    return hasher.Sum(nil)
}

// SerializeProof serializes the proof for storage or transmission
func (sp *SequenceProof) SerializeProof() ([]byte, error) {
    // Calculate total size needed
    size := 0
    size += len(calculateEntryHash(sp.Entry))  // Entry hash
    size += len(sp.WindowRoot)                 // Window root
    size += 16                                 // Window bounds (2 timestamps)
    size += 16                                 // Metadata
    size += len(sp.Metadata.PrevWindowLastHash) // Previous window hash
    for _, path := range sp.ProofPath {
        size += len(path)
    }

    buf := make([]byte, size)
    offset := 0

    // Write entry hash
    entryHash := calculateEntryHash(sp.Entry)
    copy(buf[offset:], entryHash)
    offset += len(entryHash)

    // Write window root
    copy(buf[offset:], sp.WindowRoot)
    offset += len(sp.WindowRoot)

    // Write window bounds
    binary.BigEndian.PutUint64(buf[offset:], uint64(sp.WindowBounds[0].UnixNano()))
    offset += 8
    binary.BigEndian.PutUint64(buf[offset:], uint64(sp.WindowBounds[1].UnixNano()))
    offset += 8

    // Write metadata
    binary.BigEndian.PutUint64(buf[offset:], sp.Metadata.WindowIndex)
    offset += 8
    binary.BigEndian.PutUint64(buf[offset:], sp.Metadata.WindowSize)
    offset += 8

    // Write previous window hash
    copy(buf[offset:], sp.Metadata.PrevWindowLastHash)
    offset += len(sp.Metadata.PrevWindowLastHash)

    // Write proof path
    for _, path := range sp.ProofPath {
        copy(buf[offset:], path)
        offset += len(path)
    }

    return buf[:offset], nil
}

// DeserializeProof deserializes a proof from bytes
func DeserializeProof(data []byte) (*SequenceProof, error) {
    if len(data) < 64 { // Minimum size for basic fields
        return nil, errors.New("insufficient data for proof")
    }

    offset := 0

    // Read entry hash and reconstruct entry
    entryHash := make([]byte, 32)
    copy(entryHash, data[offset:offset+32])
    offset += 32

    // Read window root
    windowRoot := make([]byte, 32)
    copy(windowRoot, data[offset:offset+32])
    offset += 32

    // Read window bounds
    startTime := time.Unix(0, int64(binary.BigEndian.Uint64(data[offset:])))
    offset += 8
    endTime := time.Unix(0, int64(binary.BigEndian.Uint64(data[offset:])))
    offset += 8

    // Read metadata
    windowIndex := binary.BigEndian.Uint64(data[offset:])
    offset += 8
    windowSize := binary.BigEndian.Uint64(data[offset:])
    offset += 8

    // Read previous window hash
    prevHash := make([]byte, 32)
    copy(prevHash, data[offset:offset+32])
    offset += 32

    // Read proof path
    var proofPath [][]byte
    for offset < len(data) {
        if offset+32 > len(data) {
            return nil, errors.New("invalid proof path data")
        }
        pathElement := make([]byte, 32)
        copy(pathElement, data[offset:offset+32])
        proofPath = append(proofPath, pathElement)
        offset += 32
    }

    sp := &SequenceProof{
        WindowRoot:   windowRoot,
        WindowBounds: [2]time.Time{startTime, endTime},
        ProofPath:    proofPath,
    }

    sp.Metadata.WindowIndex = windowIndex
    sp.Metadata.WindowSize = windowSize
    sp.Metadata.PrevWindowLastHash = prevHash

    return sp, nil
}

// VerifySequential verifies this proof follows correctly from a previous proof
func (sp *SequenceProof) VerifySequential(prevProof *SequenceProof) error {
    if prevProof == nil {
        if len(sp.Metadata.PrevWindowLastHash) > 0 {
            return errors.New("unexpected previous window hash")
        }
        return nil
    }

    // Verify windows are sequential
    if !sp.WindowBounds[0].After(prevProof.WindowBounds[1]) {
        return errors.New("non-sequential windows")
    }

    // Verify hash chain
    prevLastEntry := prevProof.Entry
    prevEntryHash := calculateEntryHash(prevLastEntry)
    if !bytes.Equal(sp.Metadata.PrevWindowLastHash, prevEntryHash) {
        return errors.New("broken hash chain between windows")
    }

    return nil
}