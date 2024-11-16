// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timechain

import (
    "testing"
    "time"
    
    "github.com/stretchr/testify/require"
)

func TestTimeWindow(t *testing.T) {
    require := require.New(t)

    // Test basic window creation
    t.Run("WindowCreation", func(t *testing.T) {
        now := time.Now()
        duration := time.Hour
        window := NewTimeWindow(now, duration, 1000)

        require.NotNil(window)
        require.Equal(now, window.StartTime)
        require.Equal(now.Add(duration), window.EndTime)
        require.Empty(window.Entries)
        require.Empty(window.RoughtimeProofs)
        require.False(window.IsClosed())
    })

    // Test entry addition and validation
    t.Run("EntryAddition", func(t *testing.T) {
        now := time.Now()
        window := NewTimeWindow(now, time.Hour, 1000)

        // Create test entry
        entry := &SequenceEntry{
            VerifiedTime: now.Add(30 * time.Minute),
            TimeProof: &RoughtimeProof{
                ServerName:    "test.server",
                Timestamp:     uint64(now.Add(30 * time.Minute).UnixNano()),
                Signature:     []byte("test-signature"),
                RadiusMicros: 1000,
                PublicKey:    []byte("test-pubkey"),
            },
            SequenceNum:  1,
            WindowHash:   []byte("test-hash"),
        }

        // Add entry
        err := window.AddEntry(entry)
        require.NoError(err)
        require.Len(window.Entries, 1)
        require.Len(window.RoughtimeProofs, 1)
    })

    // Test time boundary validation
    t.Run("TimeBoundaries", func(t *testing.T) {
        now := time.Now()
        window := NewTimeWindow(now, time.Hour, 1000)

        // Test entry before window
        earlyEntry := &SequenceEntry{
            VerifiedTime: now.Add(-1 * time.Minute),
            TimeProof:    &RoughtimeProof{},
            SequenceNum:  1,
        }
        err := window.AddEntry(earlyEntry)
        require.Error(err)

        // Test entry after window
        lateEntry := &SequenceEntry{
            VerifiedTime: now.Add(2 * time.Hour),
            TimeProof:    &RoughtimeProof{},
            SequenceNum:  2,
        }
        err = window.AddEntry(lateEntry)
        require.Error(err)

        // Test valid entry
        validEntry := &SequenceEntry{
            VerifiedTime: now.Add(30 * time.Minute),
            TimeProof:    &RoughtimeProof{},
            SequenceNum:  3,
        }
        err = window.AddEntry(validEntry)
        require.NoError(err)
    })

    // Test Merkle tree updates
    t.Run("MerkleTreeUpdates", func(t *testing.T) {
        now := time.Now()
        window := NewTimeWindow(now, time.Hour, 1000)

        // Add multiple entries
        for i := 0; i < 3; i++ {
            entry := &SequenceEntry{
                VerifiedTime: now.Add(time.Duration(i*15) * time.Minute),
                TimeProof:    &RoughtimeProof{},
                SequenceNum:  uint64(i + 1),
            }
            err := window.AddEntry(entry)
            require.NoError(err)
        }

        // Verify Merkle root exists
        root := window.GetRoot()
        require.NotEmpty(root)
    })

    // Test proof path generation
    t.Run("ProofPathGeneration", func(t *testing.T) {
        now := time.Now()
        window := NewTimeWindow(now, time.Hour, 1000)

        // Add entries
        entries := make([]*SequenceEntry, 3)
        for i := 0; i < 3; i++ {
            entry := &SequenceEntry{
                VerifiedTime: now.Add(time.Duration(i*15) * time.Minute),
                TimeProof: &RoughtimeProof{
                    ServerName:    "test.server",
                    Timestamp:     uint64(now.Add(time.Duration(i*15) * time.Minute).UnixNano()),
                    Signature:     []byte("test-signature"),
                    RadiusMicros: 1000,
                    PublicKey:    []byte("test-pubkey"),
                },
                SequenceNum:  uint64(i + 1),
            }
            err := window.AddEntry(entry)
            require.NoError(err)
            entries[i] = entry
        }

        // Generate and verify proof path for each entry
        for _, entry := range entries {
            proofPath, err := window.generateProofPath(entry)
            require.NoError(err)
            require.NotEmpty(proofPath)

            // Verify the proof path
            err = window.verifyProofPath(entry, proofPath)
            require.NoError(err)
        }
    })

    // Test window closure
    t.Run("WindowClosure", func(t *testing.T) {
        window := NewTimeWindow(time.Now(), time.Hour, 2)

        // Add entries until window is full
        for i := 0; i < 2; i++ {
            entry := &SequenceEntry{
                VerifiedTime: time.Now().Add(time.Duration(i*15) * time.Minute),
                TimeProof:    &RoughtimeProof{},
                SequenceNum:  uint64(i + 1),
            }
            err := window.AddEntry(entry)
            require.NoError(err)
        }

        // Verify window is closed
        require.True(window.IsClosed())

        // Try to add another entry
        entry := &SequenceEntry{
            VerifiedTime: time.Now().Add(45 * time.Minute),
            TimeProof:    &RoughtimeProof{},
            SequenceNum:  3,
        }
        err := window.AddEntry(entry)
        require.Error(err)
    })

    // Test duplicate entries
    t.Run("DuplicateEntries", func(t *testing.T) {
        window := NewTimeWindow(time.Now(), time.Hour, 1000)

        entry := &SequenceEntry{
            VerifiedTime: time.Now().Add(15 * time.Minute),
            TimeProof:    &RoughtimeProof{},
            SequenceNum:  1,
        }

        // Add entry first time
        err := window.AddEntry(entry)
        require.NoError(err)

        // Try to add same entry again
        err = window.AddEntry(entry)
        require.Error(err)
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
    }
}