// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timechain

import (
    "testing"
    "time"

    "github.com/stretchr/testify/require"
)

func TestWindowTransitions(t *testing.T) {
    require := require.New(t)

    // Initialize components
    rc := NewRoughtimeClient(1) // Use 1 for testing
    verifier := NewVerifier(time.Second)
    windowDuration := 5 * time.Second
    
    sequence := NewTimeSequence(
        rc,
        windowDuration,
        verifier,
        10, // maxWindows
        &SequenceConfig{
            MinEntriesPerWindow: 1,
            MaxEntriesPerWindow: 5,
            PruneThreshold: 10,
            RotationCheckInterval: windowDuration / 4,
        },
    )

    // Start sequence
    err := sequence.Start()
    require.NoError(err)
    defer sequence.Stop()

    t.Run("NormalTransition", func(t *testing.T) {
        now := time.Now()
        
        // Add entries spanning multiple windows
        for i := 0; i < 3; i++ {
            entry := &SequenceEntry{
                VerifiedTime: now.Add(time.Duration(i) * 2 * time.Second),
                TimeProof: &RoughtimeProof{
                    ServerName:    "test.server",
                    Timestamp:     uint64(now.Add(time.Duration(i) * 2 * time.Second).UnixNano()),
                    Signature:     []byte("test-sig"),
                    RadiusMicros: 1000,
                    PublicKey:    []byte("test-key"),
                },
                SequenceNum: uint64(i),
            }

            err := sequence.AddEntry(entry)
            require.NoError(err)
        }

        // Verify transitions
        transitions := sequence.GetWindowTransitions()
        require.NotEmpty(transitions)
        
        // Verify window linking
        for i := 1; i < len(sequence.windows); i++ {
            currentWindow := sequence.windows[i]
            prevWindow := sequence.windows[i-1]
            require.Equal(prevWindow.GetRoot(), currentWindow.PrevWindowHash)
        }
    })

    t.Run("GapDetection", func(t *testing.T) {
        now := time.Now()

        // Create a gap by adding entries with a time gap
        entry1 := &SequenceEntry{
            VerifiedTime: now,
            TimeProof: &RoughtimeProof{
                ServerName:    "test.server",
                Timestamp:     uint64(now.UnixNano()),
                Signature:     []byte("test-sig-1"),
                RadiusMicros: 1000,
                PublicKey:    []byte("test-key"),
            },
            SequenceNum: sequence.GetNextSequenceNum(),
        }
        err := sequence.AddEntry(entry1)
        require.NoError(err)

        // Add entry after a gap
        entry2 := &SequenceEntry{
            VerifiedTime: now.Add(15 * time.Second), // Creates gap
            TimeProof: &RoughtimeProof{
                ServerName:    "test.server",
                Timestamp:     uint64(now.Add(15 * time.Second).UnixNano()),
                Signature:     []byte("test-sig-2"),
                RadiusMicros: 1000,
                PublicKey:    []byte("test-key"),
            },
            SequenceNum: sequence.GetNextSequenceNum(),
        }
        err = sequence.AddEntry(entry2)
        require.Error(err) // Should detect gap

        // Verify gap detection
        gaps := sequence.GetDetectedGaps()
        require.NotEmpty(gaps)
        require.Equal(entry1.VerifiedTime.Add(windowDuration), gaps[0].StartTime)
    })

    t.Run("PendingEntries", func(t *testing.T) {
        now := time.Now()

        // Add entry for future window
        futureEntry := &SequenceEntry{
            VerifiedTime: now.Add(windowDuration * 2),
            TimeProof: &RoughtimeProof{
                ServerName:    "test.server",
                Timestamp:     uint64(now.Add(windowDuration * 2).UnixNano()),
                Signature:     []byte("test-sig-future"),
                RadiusMicros: 1000,
                PublicKey:    []byte("test-key"),
            },
            SequenceNum: sequence.GetNextSequenceNum(),
        }

        err := sequence.AddEntry(futureEntry)
        require.NoError(err)

        // Verify entry is pending
        pendingCount := sequence.GetPendingEntryCount()
        require.Equal(1, pendingCount)

        // Wait for window rotation
        time.Sleep(windowDuration * 2)

        // Verify entry was processed
        pendingCount = sequence.GetPendingEntryCount()
        require.Equal(0, pendingCount)

        // Verify entry was added to correct window
        entry := sequence.GetSequenceEntry(futureEntry.SequenceNum)
        require.NotNil(entry)
        require.Equal(futureEntry.VerifiedTime, entry.VerifiedTime)
    })

    t.Run("WindowLimits", func(t *testing.T) {
        now := time.Now()

        // Fill a window to capacity
        for i := 0; i < 6; i++ { // One more than MaxEntriesPerWindow
            entry := &SequenceEntry{
                VerifiedTime: now.Add(time.Duration(i) * time.Second),
                TimeProof: &RoughtimeProof{
                    ServerName:    "test.server",
                    Timestamp:     uint64(now.Add(time.Duration(i) * time.Second).UnixNano()),
                    Signature:     []byte("test-sig"),
                    RadiusMicros: 1000,
                    PublicKey:    []byte("test-key"),
                },
                SequenceNum: sequence.GetNextSequenceNum(),
            }

            err := sequence.AddEntry(entry)
            if i < 5 {
                require.NoError(err)
            } else {
                require.Error(err) // Should hit window capacity
            }
        }
    })

    t.Run("WindowPruning", func(t *testing.T) {
        initialWindowCount := len(sequence.windows)

        // Add entries until pruning is triggered
        now := time.Now()
        for i := 0; i < 15; i++ { // More than maxWindows
            entry := &SequenceEntry{
                VerifiedTime: now.Add(time.Duration(i) * windowDuration),
                TimeProof: &RoughtimeProof{
                    ServerName:    "test.server",
                    Timestamp:     uint64(now.Add(time.Duration(i) * windowDuration).UnixNano()),
                    Signature:     []byte("test-sig"),
                    RadiusMicros: 1000,
                    PublicKey:    []byte("test-key"),
                },
                SequenceNum: sequence.GetNextSequenceNum(),
            }

            err := sequence.AddEntry(entry)
            require.NoError(err)
        }

        // Verify pruning occurred
        finalWindowCount := len(sequence.windows)
        require.LessOrEqual(finalWindowCount, sequence.maxWindows)
        require.Less(finalWindowCount, initialWindowCount+15)
    })
}

func TestSequenceRecovery(t *testing.T) {
    require := require.New(t)

    // Initialize sequence
    rc := NewRoughtimeClient(1)
    verifier := NewVerifier(time.Second)
    sequence := NewTimeSequence(
        rc,
        5*time.Second,
        verifier,
        10,
        &SequenceConfig{
            MinEntriesPerWindow: 1,
            MaxEntriesPerWindow: 5,
            PruneThreshold: 10,
            RotationCheckInterval: time.Second,
        },
    )

    // Start sequence
    err := sequence.Start()
    require.NoError(err)

    // Add some entries
    now := time.Now()
    for i := 0; i < 3; i++ {
        entry := &SequenceEntry{
            VerifiedTime: now.Add(time.Duration(i) * time.Second),
            TimeProof: &RoughtimeProof{
                ServerName:    "test.server",
                Timestamp:     uint64(now.Add(time.Duration(i) * time.Second).UnixNano()),
                Signature:     []byte("test-sig"),
                RadiusMicros: 1000,
                PublicKey:    []byte("test-key"),
            },
            SequenceNum: uint64(i),
        }

        err := sequence.AddEntry(entry)
        require.NoError(err)
    }

    // Stop sequence
    err = sequence.Stop()
    require.NoError(err)

    // Start new sequence
    sequence2 := NewTimeSequence(
        rc,
        5*time.Second,
        verifier,
        10,
        &SequenceConfig{
            MinEntriesPerWindow: 1,
            MaxEntriesPerWindow: 5,
            PruneThreshold: 10,
            RotationCheckInterval: time.Second,
        },
    )

    // Start should recover state
    err = sequence2.Start()
    require.NoError(err)
    defer sequence2.Stop()

    // Verify state was recovered
    require.Equal(sequence.nextSeqNum, sequence2.nextSeqNum)
    require.Equal(len(sequence.windows), len(sequence2.windows))
}