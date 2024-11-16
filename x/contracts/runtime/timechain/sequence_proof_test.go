// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timechain

import (
    "testing"
    "time"

    "github.com/stretchr/testify/require"
    "golang.org/x/crypto/sha3"
)

func TestSequenceProof(t *testing.T) {
    require := require.New(t)

    // Create test data
    now := time.Now()
    entry := &SequenceEntry{
        VerifiedTime: now,
        TimeProof: &RoughtimeProof{
            ServerName:    "test.server",
            Timestamp:     uint64(now.UnixNano()),
            Signature:     []byte("test-signature"),
            RadiusMicros: 1000,
            PublicKey:    []byte("test-pubkey"),
        },
        SequenceNum:  1,
        WindowHash:   []byte("test-window-hash"),
    }

    windowRoot := []byte("test-root")
    bounds := [2]time.Time{now.Add(-time.Hour), now.Add(time.Hour)}
    proofPath := [][]byte{
        []byte("proof-1"),
        []byte("proof-2"),
    }

    // Test NewSequenceProof
    t.Run("NewSequenceProof", func(t *testing.T) {
        proof := NewSequenceProof(entry, windowRoot, bounds, proofPath)
        require.NotNil(proof)
        require.Equal(entry, proof.Entry)
        require.Equal(windowRoot, proof.WindowRoot)
        require.Equal(bounds, proof.WindowBounds)
        require.Equal(proofPath, proof.ProofPath)
    })

    // Test proof verification
    t.Run("VerifyProof", func(t *testing.T) {
        proof := NewSequenceProof(entry, windowRoot, bounds, proofPath)
        proof.Metadata.WindowIndex = 0
        proof.Metadata.WindowSize = 1

        verifier := NewVerifier(time.Minute, time.Hour, NewRoughtimeClient(1))
        err := proof.Verify(verifier)
        require.NoError(err)
    })

    // Test invalid cases
    t.Run("InvalidProofs", func(t *testing.T) {
        verifier := NewVerifier(time.Minute, time.Hour, NewRoughtimeClient(1))

        // Test missing entry
        t.Run("MissingEntry", func(t *testing.T) {
            proof := NewSequenceProof(nil, windowRoot, bounds, proofPath)
            err := proof.Verify(verifier)
            require.Error(err)
        })

        // Test missing window root
        t.Run("MissingRoot", func(t *testing.T) {
            proof := NewSequenceProof(entry, nil, bounds, proofPath)
            err := proof.Verify(verifier)
            require.Error(err)
        })

        // Test invalid time bounds
        t.Run("InvalidBounds", func(t *testing.T) {
            invalidBounds := [2]time.Time{now.Add(time.Hour), now.Add(-time.Hour)} // End before start
            proof := NewSequenceProof(entry, windowRoot, invalidBounds, proofPath)
            err := proof.Verify(verifier)
            require.Error(err)
        })

        // Test entry outside window
        t.Run("OutsideWindow", func(t *testing.T) {
            outsideBounds := [2]time.Time{
                now.Add(2 * time.Hour),
                now.Add(3 * time.Hour),
            }
            proof := NewSequenceProof(entry, windowRoot, outsideBounds, proofPath)
            err := proof.Verify(verifier)
            require.Error(err)
        })
    })

    // Test serialization
    t.Run("Serialization", func(t *testing.T) {
        proof := NewSequenceProof(entry, windowRoot, bounds, proofPath)
        proof.Metadata.WindowIndex = 1
        proof.Metadata.WindowSize = 2
        proof.Metadata.PrevWindowLastHash = []byte("prev-hash")

        // Serialize
        data, err := proof.SerializeProof()
        require.NoError(err)
        require.NotEmpty(data)

        // Deserialize
        deserializedProof, err := DeserializeProof(data)
        require.NoError(err)
        require.NotNil(deserializedProof)

        // Verify fields match
        require.Equal(proof.WindowRoot, deserializedProof.WindowRoot)
        require.Equal(proof.WindowBounds, deserializedProof.WindowBounds)
        require.Equal(proof.Metadata.WindowIndex, deserializedProof.Metadata.WindowIndex)
        require.Equal(proof.Metadata.WindowSize, deserializedProof.Metadata.WindowSize)
        require.Equal(proof.Metadata.PrevWindowLastHash, deserializedProof.Metadata.PrevWindowLastHash)
    })

    // Test sequential verification
    t.Run("SequentialVerification", func(t *testing.T) {
        // Create two sequential proofs
        proof1 := NewSequenceProof(entry, windowRoot, bounds, proofPath)
        
        entry2 := &SequenceEntry{
            VerifiedTime: now.Add(2 * time.Hour),
            TimeProof:    entry.TimeProof,
            SequenceNum:  2,
            WindowHash:   []byte("test-window-hash-2"),
        }
        bounds2 := [2]time.Time{
            bounds[1].Add(time.Second),
            bounds[1].Add(time.Hour),
        }
        proof2 := NewSequenceProof(entry2, windowRoot, bounds2, proofPath)
        proof2.Metadata.PrevWindowLastHash = calculateEntryHash(entry)

        // Verify sequential relationship
        err := proof2.VerifySequential(proof1)
        require.NoError(err)

        // Test broken sequence
        proof2.Metadata.PrevWindowLastHash = []byte("invalid-hash")
        err = proof2.VerifySequential(proof1)
        require.Error(err)
    })

    // Test proof hash generation
    t.Run("ProofHashing", func(t *testing.T) {
        proof := NewSequenceProof(entry, windowRoot, bounds, proofPath)
        hash := proof.GetProofHash()
        require.NotEmpty(hash)

        // Generate hash again - should be deterministic
        hash2 := proof.GetProofHash()
        require.Equal(hash, hash2)

        // Modify proof and verify hash changes
        proof.WindowRoot = []byte("different-root")
        hash3 := proof.GetProofHash()
        require.NotEqual(hash, hash3)
    })
}

// Helper function to create test entries
func createTestEntry(t time.Time, seq uint64) *SequenceEntry {
    return &SequenceEntry{
        VerifiedTime: t,
        TimeProof: &RoughtimeProof{
            ServerName:    "test.server",
            Timestamp:     uint64(t.UnixNano()),
            Signature:     []byte("test-signature"),
            RadiusMicros: 1000,
            PublicKey:    []byte("test-pubkey"),
        },
        SequenceNum:  seq,
        WindowHash:   []byte("test-hash"),
    }
}

// Helper function to create a chain of proofs
func createProofChain(t *testing.T, count int) []*SequenceProof {
    require := require.New(t)
    
    now := time.Now()
    proofs := make([]*SequenceProof, count)
    
    for i := 0; i < count; i++ {
        entry := createTestEntry(now.Add(time.Duration(i)*time.Hour), uint64(i))
        bounds := [2]time.Time{
            now.Add(time.Duration(i) * time.Hour),
            now.Add(time.Duration(i+1) * time.Hour),
        }
        
        proof := NewSequenceProof(
            entry,
            []byte(fmt.Sprintf("root-%d", i)),
            bounds,
            [][]byte{[]byte("proof")},
        )
        
        if i > 0 {
            proof.Metadata.PrevWindowLastHash = calculateEntryHash(proofs[i-1].Entry)
        }
        
        proofs[i] = proof
    }
    
    return proofs
}