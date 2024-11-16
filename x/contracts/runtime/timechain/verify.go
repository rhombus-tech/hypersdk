// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timechain

import (
    "bytes"
    "crypto/ed25519"
    "encoding/binary"
    "errors"
    "fmt"
    "time"
    
    "golang.org/x/crypto/sha3"
)

var (
    ErrSequenceViolation      = errors.New("time sequence violation")
    ErrHashChainBroken       = errors.New("hash chain broken")
    ErrInvalidProof          = errors.New("invalid time proof")
    ErrOutsideTimeWindow     = errors.New("time outside window")
    ErrMissingProof          = errors.New("missing time proof")
    ErrInvalidSignature      = errors.New("invalid proof signature")
    ErrInsufficientProofs    = errors.New("insufficient time proofs")
    ErrTimestampTooOld       = errors.New("timestamp too old")
    ErrTimestampInFuture     = errors.New("timestamp in future")
)

// Verifier handles verification of time sequence entries
type Verifier struct {
    tolerance    time.Duration
    maxAge       time.Duration
    roughtime    *RoughtimeClient
}

func NewVerifier(tolerance time.Duration, maxAge time.Duration, rc *RoughtimeClient) *Verifier {
    return &Verifier{
        tolerance: tolerance,
        maxAge:    maxAge,
        roughtime: rc,
    }
}

// VerifySequence verifies a sequence of time entries
func (v *Verifier) VerifySequence(entries []*SequenceEntry) error {
    if len(entries) == 0 {
        return nil
    }

    // Verify the first entry independently
    if err := v.verifyEntry(entries[0], nil); err != nil {
        return fmt.Errorf("first entry invalid: %w", err)
    }

    // Verify subsequent entries in relation to previous ones
    for i := 1; i < len(entries); i++ {
        if err := v.verifyEntry(entries[i], entries[i-1]); err != nil {
            return fmt.Errorf("entry %d invalid: %w", i, err)
        }
    }

    return nil
}

// verifyEntry verifies a single sequence entry
func (v *Verifier) verifyEntry(entry, prev *SequenceEntry) error {
    // Verify the entry has required fields
    if err := v.validateEntryFields(entry); err != nil {
        return err
    }

    // Verify time proof
    if err := v.verifyTimeProof(entry.TimeProof); err != nil {
        return fmt.Errorf("proof verification failed: %w", err)
    }

    // Verify time is reasonable
    if err := v.verifyTimeRange(entry.VerifiedTime); err != nil {
        return err
    }

    // If this is not the first entry, verify sequence
    if prev != nil {
        if err := v.verifySequenceRelation(entry, prev); err != nil {
            return err
        }
    }

    // Verify hash chain
    if err := v.verifyHashChain(entry, prev); err != nil {
        return err
    }

    return nil
}

// validateEntryFields checks if all required fields are present
func (v *Verifier) validateEntryFields(entry *SequenceEntry) error {
    if entry == nil {
        return errors.New("nil entry")
    }
    if entry.TimeProof == nil {
        return ErrMissingProof
    }
    if entry.VerifiedTime.IsZero() {
        return errors.New("missing verified time")
    }
    return nil
}

// verifyTimeProof verifies the Roughtime proof
func (v *Verifier) verifyTimeProof(proof *RoughtimeProof) error {
    if proof == nil {
        return ErrMissingProof
    }

    // Create message hash
    hasher := sha3.New256()
    timeBytes := make([]byte, 8)
    binary.BigEndian.PutUint64(timeBytes, proof.Timestamp)
    hasher.Write(timeBytes)

    radiusBytes := make([]byte, 4)
    binary.BigEndian.PutUint32(radiusBytes, proof.RadiusMicros)
    hasher.Write(radiusBytes)

    messageHash := hasher.Sum(nil)

    // Verify signature
    if !ed25519.Verify(proof.PublicKey, messageHash, proof.Signature) {
        return ErrInvalidSignature
    }

    return nil
}

// verifyTimeRange checks if the timestamp is within acceptable bounds
func (v *Verifier) verifyTimeRange(timestamp time.Time) error {
    now := time.Now()

    // Check if timestamp is too old
    if now.Sub(timestamp) > v.maxAge {
        return ErrTimestampTooOld
    }

    // Check if timestamp is in future (with tolerance)
    if timestamp.After(now.Add(v.tolerance)) {
        return ErrTimestampInFuture
    }

    return nil
}

// verifySequenceRelation verifies the relationship between consecutive entries
func (v *Verifier) verifySequenceRelation(current, prev *SequenceEntry) error {
    // Verify time ordering
    if !current.VerifiedTime.After(prev.VerifiedTime) {
        return ErrSequenceViolation
    }

    // Verify sequence numbers
    if current.SequenceNum != prev.SequenceNum+1 {
        return errors.New("invalid sequence number")
    }

    return nil
}

// verifyHashChain verifies the hash chain integrity
func (v *Verifier) verifyHashChain(current, prev *SequenceEntry) error {
    if prev != nil {
        expectedHash := calculateEntryHash(prev)
        if !bytes.Equal(current.PrevHash, expectedHash) {
            return ErrHashChainBroken
        }
    } else if len(current.PrevHash) != 0 {
        // First entry should have empty PrevHash
        return errors.New("first entry has non-empty previous hash")
    }

    // Verify current entry's hash matches its WindowHash
    currentHash := calculateEntryHash(current)
    if !bytes.Equal(current.WindowHash, currentHash) {
        return errors.New("invalid window hash")
    }

    return nil
}

// calculateEntryHash generates a hash for a sequence entry
func calculateEntryHash(entry *SequenceEntry) []byte {
    hasher := sha3.New256()

    // Hash verified time
    timeBytes := make([]byte, 8)
    binary.BigEndian.PutUint64(timeBytes, uint64(entry.VerifiedTime.UnixNano()))
    hasher.Write(timeBytes)

    // Hash time proof
    if entry.TimeProof != nil {
        proofHash := entry.TimeProof.GetProofHash()
        hasher.Write(proofHash)
    }

    // Hash previous hash if exists
    if len(entry.PrevHash) > 0 {
        hasher.Write(entry.PrevHash)
    }

    // Hash sequence number
    seqBytes := make([]byte, 8)
    binary.BigEndian.PutUint64(seqBytes, entry.SequenceNum)
    hasher.Write(seqBytes)

    return hasher.Sum(nil)
}

// VerifyTimeWindow verifies all entries in a time window
func (v *Verifier) VerifyTimeWindow(window *TimeWindow) error {
    if window == nil {
        return errors.New("nil time window")
    }

    // Verify window boundaries
    if window.EndTime.Before(window.StartTime) {
        return errors.New("invalid window boundaries")
    }

    // Verify all entries are within window
    for i, entry := range window.Entries {
        if entry.VerifiedTime.Before(window.StartTime) || 
           entry.VerifiedTime.After(window.EndTime) {
            return fmt.Errorf("entry %d outside window bounds", i)
        }
    }

    // Verify sequence integrity within window
    return v.VerifySequence(window.Entries)
}

// VerifyProofBatch verifies a batch of time proofs
func (v *Verifier) VerifyProofBatch(proofs []*RoughtimeProof) error {
    if len(proofs) < v.roughtime.minServers {
        return ErrInsufficientProofs
    }

    for i, proof := range proofs {
        if err := v.verifyTimeProof(proof); err != nil {
            return fmt.Errorf("proof %d invalid: %w", i, err)
        }
    }

    return nil
}