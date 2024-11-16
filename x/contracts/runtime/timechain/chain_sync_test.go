// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timechain

import (
    "testing"
    "time"

    "github.com/ava-labs/avalanchego/ids"
    "github.com/stretchr/testify/require"
)

func TestBlockSync(t *testing.T) {
    require := require.New(t)

    // Setup basic sequence
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
            PruneThreshold:      10,
            RotationCheckInterval: time.Second,
        },
    )

    err := sequence.Start()
    require.NoError(err)
    defer sequence.Stop()

    t.Run("NormalBlockProgression", func(t *testing.T) {
        now := time.Now()

        // Add some entries and blocks
        for i := uint64(1); i <= 5; i++ {
            // Add entry
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
            require.NoError(err)

            // Commit block
            blockID := ids.GenerateTestID()
            err = sequence.OnBlockCommit(i, blockID, now.Add(time.Duration(i)*time.Second))
            require.NoError(err)
        }

        // Verify window mappings
        window := sequence.currentWindow
        require.NotNil(window)

        blockRange, err := sequence.blockSync.GetBlockRange(window)
        require.NoError(err)
        require.True(blockRange > 0)
    })

    t.Run("ChainReorganization", func(t *testing.T) {
        now := time.Now()

        // Add blocks and entries
        var lastBlockID ids.ID
        for i := uint64(6); i <= 10; i++ {
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
            require.NoError(err)

            lastBlockID = ids.GenerateTestID()
            err = sequence.OnBlockCommit(i, lastBlockID, now.Add(time.Duration(i)*time.Second))
            require.NoError(err)
        }

        // Store state before reorg
        originalNextSeqNum := sequence.nextSeqNum
        originalWindowCount := len(sequence.windows)

        // Trigger reorg
        reorgHeight := uint64(8)
        err := sequence.OnBlockReorg(reorgHeight)
        require.NoError(err)

        // Verify state was rolled back
        require.Less(sequence.nextSeqNum, originalNextSeqNum)
        require.Less(len(sequence.windows), originalWindowCount)

        // Verify we can continue after reorg
        for i := reorgHeight; i <= 12; i++ {
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
            require.NoError(err)

            blockID := ids.GenerateTestID()
            err = sequence.OnBlockCommit(i, blockID, now.Add(time.Duration(i)*time.Second))
            require.NoError(err)
        }
    })

    t.Run("InvalidBlockHeight", func(t *testing.T) {
        now := time.Now()

        // Try to commit block with invalid height
        err := sequence.OnBlockCommit(1, ids.GenerateTestID(), now)
        require.Error(err)
        require.Contains(err.Error(), "invalid block height")
    })

    t.Run("RollbackPointCleanup", func(t *testing.T) {
        // Add many blocks to trigger cleanup
        now := time.Now()
        for i := uint64(13); i <= 30; i++ {
            blockID := ids.GenerateTestID()
            err := sequence.OnBlockCommit(i, blockID, now.Add(time.Duration(i)*time.Second))
            require.NoError(err)
        }

        // Trigger cleanup
        sequence.blockSync.CleanupRollbackPoints(25)

        // Verify old rollback points were removed
        for height := range sequence.blockSync.rollbackPoints {
            require.GreaterOrEqual(height, uint64(25))
        }
    })

    t.Run("WindowBlockMapping", func(t *testing.T) {
        // Get current window
        currentWindow := sequence.currentWindow
        require.NotNil(currentWindow)

        // Get block range for window
        start, end, err := sequence.blockSync.GetBlockRange(currentWindow)
        require.NoError(err)
        require.True(start > 0)
        require.True(end >= start)

        // Verify window retrieval by block height
        window, err := sequence.blockSync.GetWindowForBlock(start)
        require.NoError(err)
        require.Equal(currentWindow, window)
    })

    t.Run("ConcurrentBlockCommits", func(t *testing.T) {
        now := time.Now()
        done := make(chan error, 10)

        // Commit blocks concurrently
        for i := uint64(31); i <= 40; i++ {
            go func(height uint64) {
                blockID := ids.GenerateTestID()
                err := sequence.OnBlockCommit(
                    height,
                    blockID,
                    now.Add(time.Duration(height)*time.Second),
                )
                done <- err
            }(i)
        }

        // Collect results
        for i := 0; i < 10; i++ {
            err := <-done
            require.NoError(err)
        }
    })

    t.Run("DeepStateRollback", func(t *testing.T) {
        // Create a snapshot of current state
        originalWindows := make([]*TimeWindow, len(sequence.windows))
        copy(originalWindows, sequence.windows)
        originalNextSeqNum := sequence.nextSeqNum

        // Trigger deep rollback
        err := sequence.OnBlockReorg(35)
        require.NoError(err)

        // Verify state was rolled back
        require.Less(len(sequence.windows), len(originalWindows))
        require.Less(sequence.nextSeqNum, originalNextSeqNum)

        // Verify window integrity after rollback
        for i := 1; i < len(sequence.windows); i++ {
            currentWindow := sequence.windows[i]
            prevWindow := sequence.windows[i-1]
            require.Equal(prevWindow.GetRoot(), currentWindow.PrevWindowHash)
        }
    })
}

func TestBlockSyncEdgeCases(t *testing.T) {
    require := require.New(t)

    // Setup sequence
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
            PruneThreshold:      10,
            RotationCheckInterval: time.Second,
        },
    )

    err := sequence.Start()
    require.NoError(err)
    defer sequence.Stop()

    t.Run("RollbackWithoutRollbackPoint", func(t *testing.T) {
        err := sequence.OnBlockReorg(1)
        require.Error(err)
        require.Contains(err.Error(), "no rollback point")
    })

    t.Run("BlockCommitBeforeStart", func(t *testing.T) {
        sequence2 := NewTimeSequence(
            rc,
            5*time.Second,
            verifier,
            10,
            &SequenceConfig{
                MinEntriesPerWindow: 1,
                MaxEntriesPerWindow: 5,
                PruneThreshold:      10,
                RotationCheckInterval: time.Second,
            },
        )

        err := sequence2.OnBlockCommit(1, ids.GenerateTestID(), time.Now())
        require.Error(err)
        require.Contains(err.Error(), "block sync not initialized")
    })

    t.Run("WindowMappingForNonexistentBlock", func(t *testing.T) {
        _, err := sequence.blockSync.GetWindowForBlock(9999)
        require.Error(err)
        require.Contains(err.Error(), "no window found")
    })
}