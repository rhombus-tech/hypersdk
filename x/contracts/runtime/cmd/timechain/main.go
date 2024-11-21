package main

import (
    "context"
    "encoding/base64"
    "crypto/ed25519"
    "log"
    "os"
    "os/signal"
    "sync"
    "syscall"
    "time"
)

func main() {
    // Initialize verifier and config
    verifier := NewVerifier(time.Second)
    
    config := &ShardConfig{
        MinRoughtimeResponses: 2,
        MaxEntriesPerShard: 10000,
        SyncInterval: time.Minute * 5,
        MaxShardDrift: time.Second * 2,
        WindowDuration: time.Hour,
        EnableCrossShardValidation: true,
        MaxRoughtimeRadius: time.Millisecond * 100,
    }

    // Define Roughtime servers
    roughServers := []RoughtimeServer{
        {
            Name:    "Cloudflare",
            Address: "roughtime.cloudflare.com:2002",
            PublicKey: mustDecodeKey("gD63hSj3ScS+wuOeGrubXlq35N1c5Lby/S+T7MNTjxo="),
            Region:  "GLOBAL",
            Priority: 1,
        },
        // Add other servers as needed
    }

    // Create shard manager
    shardManager := NewShardManager(config, verifier)

    // Create shards for different timezones
    timezones := []string{
        "America/New_York",
        "Europe/London",
        "Asia/Tokyo",
    }

    // Initialize shards
    for _, tz := range timezones {
        location, err := time.LoadLocation(tz)
        if err != nil {
            log.Printf("Warning: Failed to load timezone %s: %v", tz, err)
            continue
        }

        shard, err := shardManager.CreateShard(location, roughServers)
        if err != nil {
            log.Printf("Warning: Failed to create shard for %s: %v", tz, err)
            continue
        }
        log.Printf("Created shard for timezone: %s", tz)
    }

    // Create context for graceful shutdown
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // Create WaitGroup for synchronization
    var wg sync.WaitGroup

    // Start background tasks
    wg.Add(1)
    go func() {
        defer wg.Done()
        runMaintenanceTasks(ctx, shardManager)
    }()

    // Start verification tasks
    wg.Add(1)
    go func() {
        defer wg.Done()
        runVerificationTasks(ctx, shardManager)
    }()

    // Setup signal handling for graceful shutdown
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

    // Wait for shutdown signal
    sig := <-sigChan
    log.Printf("Received signal %v, initiating shutdown...", sig)

    // Trigger graceful shutdown
    cancel()

    // Wait for all tasks to complete
    wg.Wait()

    log.Println("Shutdown complete")
}

// Maintenance tasks
func runMaintenanceTasks(ctx context.Context, sm *ShardManager) {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            // Verify shard consistency
            if err := sm.VerifyShardConsistency(); err != nil {
                log.Printf("Shard consistency check failed: %v", err)
            }

            // Optional: Prune old entries
            // sm.PruneOldEntries()

            // Optional: Log statistics
            logShardStats(sm)
        }
    }
}

// Verification tasks
func runVerificationTasks(ctx context.Context, sm *ShardManager) {
    ticker := time.NewTicker(time.Minute)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            // Perform periodic verifications
            for _, shard := range sm.shards {
                err := shard.syncWithRoughtime()
                if err != nil {
                    log.Printf("Failed to sync shard %s with Roughtime: %v", 
                        shard.ID, err)
                }
            }
        }
    }
}

// Helper function to log shard statistics
func logShardStats(sm *ShardManager) {
    sm.mutex.RLock()
    defer sm.mutex.RUnlock()

    for _, shard := range sm.shards {
        log.Printf("Shard %s stats: Entries=%d, Proofs=%d, CrossRefs=%d", 
            shard.ID,
            shard.stats.EntryCount,
            shard.stats.ProofCount,
            shard.stats.CrossShardRefs)
    }
}

// Helper function to decode base64 public keys
func mustDecodeKey(b64Key string) ed25519.PublicKey {
    key, err := base64.StdEncoding.DecodeString(b64Key)
    if err != nil {
        panic("invalid public key: " + err.Error())
    }
    return ed25519.PublicKey(key)
}

    // Your application's main loop or server initialization would go here
}
