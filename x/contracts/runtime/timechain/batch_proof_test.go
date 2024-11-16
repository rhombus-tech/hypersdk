// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timechain

import (
    "testing"
    "time"

    "github.com/ava-labs/avalanchego/ids"
    "github.com/stretchr/testify/require"
)

func TestProofBatchGeneration(t *testing.T) {
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

    // Add test entries
    now := time.Now()
    for i := uint64(0); i < 10; i++ {
        entry := &SequenceEntry{
            VerifiedTime: now.Add(time.Duration(i) * time.Second),
            TimeProof: &RoughtimeProof{
                ServerName:    "test.server",
                Timestamp:     uint64(now.Add(time.Duration(i) * time.Second).UnixNano()),
                Signature:     []byte("test-sig"),
                RadiusMicros: 1000,
                PublicKey:    []byte("test-key"),
            },
            SequenceNum: i,
        }
        err := sequence.AddEntry(entry)
        require.NoError(err)

        // Add block for each entry
        err = sequence.OnBlockCommit(i+1, ids.GenerateTestID(), now.Add(time.Duration(i)*time.Second))
        require.NoError(err)
    }

    // Create batch generator
    batchGen := NewProofBatchGenerator(sequence, 4)

    t.Run("BasicBatchGeneration", func(t *testing.T) {
        request := &BatchProofRequest{
            SequenceNums: []uint64{0, 1, 2},
            MaxBatchSize: 5,
            AllowPartial: false,
        }

        response, err := batchGen.GenerateProofBatch(request)
        require.NoError(err)
        require.NotNil(response)
        require.Len(response.Proofs, 3)
        require.Equal(uint64(3), response.GeneratedCount)
        require.Equal(uint64(0), response.FailedCount)
        require.NotEmpty(response.BatchRoot)

        // Verify proofs are ordered
        for i, proof := range response.Proofs {
            require.Equal(uint64(i), proof.Entry.SequenceNum)
        }
    })

    t.Run("TimeRangeFiltering", func(t *testing.T) {
        startTime := now.Add(2 * time.Second)
        endTime := now.Add(5 * time.Second)
        request := &BatchProofRequest{
            SequenceNums: []uint64{0, 1, 2, 3, 4, 5},
            StartTime:    &startTime,
            EndTime:      &endTime,
            MaxBatchSize: 10,
            AllowPartial: true,
        }

        response, err := batchGen.GenerateProofBatch(request)
        require.NoError(err)
        require.NotNil(response)

        // Verify all proofs are within time range
        for _, proof := range response.Proofs {
            require.True(proof.Entry.VerifiedTime.After(startTime) || 
                       proof.Entry.VerifiedTime.Equal(startTime))
            require.True(proof.Entry.VerifiedTime.Before(endTime) || 
                       proof.Entry.VerifiedTime.Equal(endTime))
        }
    })

    t.Run("BlockHeightFiltering", func(t *testing.T) {
        blockHeight := uint64(5)
        request := &BatchProofRequest{
            SequenceNums: []uint64{0, 1, 2, 3, 4, 5, 6},
            BlockHeight: &blockHeight,
            MaxBatchSize: 10,
            AllowPartial: true,
        }

        response, err := batchGen.GenerateProofBatch(request)
        require.NoError(err)
        require.NotNil(response)

        // Verify all proofs are from blocks <= blockHeight
        for _, proof := range response.Proofs {
            window := sequence.findContainingWindow(proof.Entry)
            require.NotNil(window)
            blockStart, _, err := sequence.blockSync.GetBlockRange(window)
            require.NoError(err)
            require.LessOrEqual(blockStart, blockHeight)
        }
    })

    t.Run("BatchSizeLimits", func(t *testing.T) {
        request := &BatchProofRequest{
            SequenceNums: []uint64{0, 1, 2},
            MaxBatchSize: 2,
            AllowPartial: false,
        }

        _, err := batchGen.GenerateProofBatch(request)
        require.Error(err)
        require.ErrorIs(err, ErrBatchTooLarge)
    })

    t.Run("PartialBatchCompletion", func(t *testing.T) {
        request := &BatchProofRequest{
            SequenceNums: []uint64{0, 1, 99}, // 99 doesn't exist
            MaxBatchSize: 5,
            AllowPartial: true,
        }

        response, err := batchGen.GenerateProofBatch(request)
        require.NoError(err)
        require.NotNil(response)
        require.Equal(uint64(2), response.GeneratedCount)
        require.Equal(uint64(1), response.FailedCount)
    })

    t.Run("ConcurrentBatchGeneration", func(t *testing.T) {
        // Create multiple concurrent requests
        requests := make([]*BatchProofRequest, 5)
        for i := 0; i < 5; i++ {
            requests[i] = &BatchProofRequest{
                SequenceNums: []uint64{0, 1, 2},
                MaxBatchSize: 5,
                AllowPartial: false,
            }
        }

        // Process requests concurrently
        results := make(chan error, len(requests))
        for _, req := range requests {
            go func(r *BatchProofRequest) {
                _, err := batchGen.GenerateProofBatch(r)
                results <- err
            }(req)
        }

        // Collect results
        for i := 0; i < len(requests); i++ {
            err := <-results
            require.NoError(err)
        }
    })

    t.Run("BatchRootConsistency", func(t *testing.T) {
        request := &BatchProofRequest{
            SequenceNums: []uint64{0, 1, 2},
            MaxBatchSize: 5,
            AllowPartial: false,
        }

        // Generate same batch twice
        response1, err := batchGen.GenerateProofBatch(request)
        require.NoError(err)
        response2, err := batchGen.GenerateProofBatch(request)
        require.NoError(err)

        // Verify batch roots are identical
        require.Equal(response1.BatchRoot, response2.BatchRoot)
    })

    t.Run("InvalidRequests", func(t *testing.T) {
        // Empty sequence numbers
        _, err := batchGen.GenerateProofBatch(&BatchProofRequest{
            SequenceNums: []uint64{},
            MaxBatchSize: 5,
        })
        require.Error(err)

        // Invalid time range
        endTime := now
        startTime := now.Add(time.Hour)
        _, err = batchGen.GenerateProofBatch(&BatchProofRequest{
            SequenceNums: []uint64{0, 1},
            StartTime:    &startTime,
            EndTime:      &endTime,
            MaxBatchSize: 5,
        })
        require.Error(err)
    })

    t.Run("BatchTimeRange", func(t *testing.T) {
        request := &BatchProofRequest{
            SequenceNums: []uint64{1, 3, 5},
            MaxBatchSize: 5,
            AllowPartial: false,
        }

        response, err := batchGen.GenerateProofBatch(request)
        require.NoError(err)
        require.NotNil(response)

        // Verify time range encompasses all proofs
        for _, proof := range response.Proofs {
            require.True(proof.Entry.VerifiedTime.After(response.TimeRange[0]) || 
                       proof.Entry.VerifiedTime.Equal(response.TimeRange[0]))
            require.True(proof.Entry.VerifiedTime.Before(response.TimeRange[1]) || 
                       proof.Entry.VerifiedTime.Equal(response.TimeRange[1]))
        }
    })
}