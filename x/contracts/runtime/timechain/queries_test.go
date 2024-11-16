// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timechain

import (
    "testing"
    "time"

    "github.com/ava-labs/avalanchego/ids"
    "github.com/stretchr/testify/require"
)

func TestQueries(t *testing.T) {
    require := require.New(t)

    // Setup sequence with some data
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

    // Add some test data
    now := time.Now()
    for i := uint64(1); i <= 10; i++ {
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
            SequenceNum: i - 1,
        }
        err := sequence.AddEntry(entry)
        require.NoError(err)

        // Commit block
        err = sequence.OnBlockCommit(i, ids.GenerateTestID(), now.Add(time.Duration(i)*time.Second))
        require.NoError(err)
    }

    t.Run("TimeRangeQuery", func(t *testing.T) {
        // Query middle range
        start := now.Add(3 * time.Second)
        end := now.Add(7 * time.Second)
        
        result, err := sequence.QueryTimeRange(start, end)
        require.NoError(err)
        require.NotEmpty(result.Windows)
        require.NotEmpty(result.Entries)
        require.NotEmpty(result.Proofs)

        // Verify entries are within range
        for _, entry := range result.Entries {
            require.True(entry.VerifiedTime.After(start))
            require.True(entry.VerifiedTime.Before(end))
        }
    })

    t.Run("BatchProofGeneration", func(t *testing.T) {
        request := &BatchProofRequest{
            SequenceNums: []uint64{1, 2, 3},
            BlockHeight: 5,
            MaxBatchSize: 10,
        }

        proofs, err := sequence.GenerateProofBatch(request)
        require.NoError(err)
        require.Len(proofs, 3)

        // Verify each proof
        for i, proof := range proofs {
            require.Equal(uint64(i+1), proof.Entry.SequenceNum)
            require.NotEmpty(proof.ProofPath)
        }
    })

    t.Run("HistoricalWindowAccess", func(t *testing.T) {
        query := &HistoricalQuery{
            StartTime:     now.Add(2 * time.Second),
            EndTime:       now.Add(6 * time.Second),
            BlockHeight:   5,
            IncludeProofs: true,
        }

        result, err := sequence.QueryHistoricalWindows(query)
        require.NoError(err)
        require.NotEmpty(result.Windows)
        require.NotEmpty(result.Entries)
        require.NotEmpty(result.Proofs)

        // Verify windows are within range and before block height
        for _, window := range result.Windows {
            blockStart, _, err := sequence.blockSync.GetBlockRange(window)
            require.NoError(err)
            require.LessOrEqual(blockStart, query.BlockHeight)
        }
    })

    // ... more tests ...
}