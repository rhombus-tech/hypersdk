package timechain

import (
    "crypto/ed25519"
    "errors"
    "fmt"
    "sort"
    "sync"
    "time"

    "github.com/cloudflare/roughtime"
    "golang.org/x/exp/maps"
)

var (
    ErrShardExists        = errors.New("shard already exists")
    ErrShardNotFound      = errors.New("shard not found")
    ErrInvalidShardTime   = errors.New("invalid shard time")
    ErrInsufficientProofs = errors.New("insufficient roughtime proofs")
    ErrShardOverflow      = errors.New("shard entry limit exceeded")
    ErrTimeDrift         = errors.New("excessive time drift detected")
)

// ShardID uniquely identifies a timezone shard
type ShardID string

// ShardConfig defines the configuration for shard management
type ShardConfig struct {
    // Minimum number of Roughtime responses required per shard
    MinRoughtimeResponses uint `json:"minRoughtimeResponses"`
    
    // Maximum entries per shard before splitting
    MaxEntriesPerShard uint64 `json:"maxEntriesPerShard"`
    
    // How often to sync with Roughtime servers
    SyncInterval time.Duration `json:"syncInterval"`
    
    // Maximum acceptable time drift between shards
    MaxShardDrift time.Duration `json:"maxShardDrift"`
    
    // Time window for grouping related entries
    WindowDuration time.Duration `json:"windowDuration"`
    
    // Whether to enable cross-shard validation
    EnableCrossShardValidation bool `json:"enableCrossShardValidation"`
    
    // Maximum radius for Roughtime proofs
    MaxRoughtimeRadius time.Duration `json:"maxRoughtimeRadius"`
}

// RoughtimeServer represents a Roughtime server configuration
type RoughtimeServer struct {
    Name      string             `json:"name"`
    Address   string             `json:"address"`
    PublicKey ed25519.PublicKey  `json:"publicKey"`
    Region    string             `json:"region"`
    Priority  uint8              `json:"priority"`
}

// Shard represents a timezone-specific shard
type Shard struct {
    ID             ShardID
    Location       *time.Location
    Sequence       *TimeSequence
    RoughtimeServers []RoughtimeServer
    
    // Internal state
    entries        map[uint64]*SequenceEntry
    timeProofs     map[uint64]*RoughtimeProof
    windows        []*TimeWindow
    lastSync       time.Time
    
    // Cross-shard references
    references     map[ShardID][]CrossShardRef
    
    // Statistics
    stats          ShardStats
    
    config         *ShardConfig
    mutex          sync.RWMutex
}

// ShardStats tracks operational statistics for a shard
type ShardStats struct {
    EntryCount        uint64
    ProofCount        uint64
    LastSyncTime      time.Time
    AvgSyncLatency    time.Duration
    RoughtimeFailures uint64
    CrossShardRefs    uint64
}

// CrossShardRef represents a reference between entries in different shards
type CrossShardRef struct {
    SourceShard    ShardID
    TargetShard    ShardID
    SourceSeqNum   uint64
    TargetSeqNum   uint64
    TimeDifference time.Duration
    Proof          *RoughtimeProof
}

// ShardManager manages timezone-based shards
type ShardManager struct {
    shards    map[ShardID]*Shard
    config    *ShardConfig
    verifier  *Verifier
    mutex     sync.RWMutex
}

// NewShardManager creates a new shard manager
func NewShardManager(config *ShardConfig, verifier *Verifier) *ShardManager {
    if config == nil {
        config = &ShardConfig{
            MinRoughtimeResponses:      3,
            MaxEntriesPerShard:         10000,
            SyncInterval:               time.Minute * 5,
            MaxShardDrift:              time.Second * 2,
            WindowDuration:             time.Hour,
            EnableCrossShardValidation: true,
            MaxRoughtimeRadius:         time.Millisecond * 100,
        }
    }

    return &ShardManager{
        shards:   make(map[ShardID]*Shard),
        config:   config,
        verifier: verifier,
    }
}

// CreateShard creates a new timezone shard
func (sm *ShardManager) CreateShard(location *time.Location, servers []RoughtimeServer) (*Shard, error) {
    sm.mutex.Lock()
    defer sm.mutex.Unlock()

    id := ShardID(location.String())
    
    if _, exists := sm.shards[id]; exists {
        return nil, ErrShardExists
    }

    // Create new sequence for the shard
    sequence := NewTimeSequence(
        NewRoughtimeClient(int(sm.config.MinRoughtimeResponses)),
        sm.config.WindowDuration,
        sm.verifier,
        10, // maxWindows
        nil,
    )

    shard := &Shard{
        ID:              id,
        Location:        location,
        Sequence:        sequence,
        RoughtimeServers: servers,
        entries:         make(map[uint64]*SequenceEntry),
        timeProofs:      make(map[uint64]*RoughtimeProof),
        references:      make(map[ShardID][]CrossShardRef),
        config:          sm.config,
    }

    // Initialize Roughtime synchronization
    if err := shard.initializeRoughtime(); err != nil {
        return nil, fmt.Errorf("failed to initialize roughtime: %w", err)
    }

    sm.shards[id] = shard
    return shard, nil
}

// initializeRoughtime initializes Roughtime synchronization for the shard
func (s *Shard) initializeRoughtime() error {
    s.mutex.Lock()
    defer s.mutex.Unlock()

    results := make([]roughtime.Result, 0, len(s.RoughtimeServers))
    
    // Query all configured Roughtime servers
    for _, server := range s.RoughtimeServers {
        result, err := roughtime.QueryServer(server.Address, server.PublicKey)
        if err != nil {
            s.stats.RoughtimeFailures++
            continue
        }
        results = append(results, result)
    }

    if uint(len(results)) < s.config.MinRoughtimeResponses {
        return ErrInsufficientProofs
    }

    // Create initial time proof
    proof := createRoughtimeProof(results, s.Location)
    s.timeProofs[0] = proof
    s.lastSync = time.Now()
    
    return nil
}

// AddEntry adds a new entry to the appropriate shard
func (sm *ShardManager) AddEntry(entry *SequenceEntry) error {
    // Determine target shard
    shard := sm.findTargetShard(entry.VerifiedTime)
    if shard == nil {
        return ErrShardNotFound
    }

    // Add entry to shard
    if err := shard.AddEntry(entry); err != nil {
        return fmt.Errorf("failed to add entry to shard: %w", err)
    }

    // Handle cross-shard validation if enabled
    if sm.config.EnableCrossShardValidation {
        if err := sm.updateCrossShardRefs(shard, entry); err != nil {
            return fmt.Errorf("failed to update cross-shard references: %w", err)
        }
    }

    return nil
}

// AddEntry adds an entry to a specific shard
func (s *Shard) AddEntry(entry *SequenceEntry) error {
    s.mutex.Lock()
    defer s.mutex.Unlock()

    // Check shard capacity
    if uint64(len(s.entries)) >= s.config.MaxEntriesPerShard {
        return ErrShardOverflow
    }

    // Verify entry time with Roughtime
    if err := s.verifyEntryTime(entry); err != nil {
        return fmt.Errorf("time verification failed: %w", err)
    }

    // Add to sequence
    if err := s.Sequence.AddEntry(entry); err != nil {
        return fmt.Errorf("sequence addition failed: %w", err)
    }

    // Store entry
    s.entries[entry.SequenceNum] = entry
    s.stats.EntryCount++

    return nil
}

// verifyEntryTime verifies an entry's time against Roughtime
func (s *Shard) verifyEntryTime(entry *SequenceEntry) error {
    // Check if we need to refresh Roughtime sync
    if time.Since(s.lastSync) > s.config.SyncInterval {
        if err := s.syncWithRoughtime(); err != nil {
            return fmt.Errorf("roughtime sync failed: %w", err)
        }
    }

    results := make([]roughtime.Result, 0, len(s.RoughtimeServers))
    
    // Query Roughtime servers
    for _, server := range s.RoughtimeServers {
        result, err := roughtime.QueryServer(server.Address, server.PublicKey)
        if err != nil {
            s.stats.RoughtimeFailures++
            continue
        }
        results = append(results, result)
    }

    if uint(len(results)) < s.config.MinRoughtimeResponses {
        return ErrInsufficientProofs
    }

    // Verify time is within acceptable range
    median := calculateMedianTime(results)
    drift := entry.VerifiedTime.Sub(median)
    if abs(drift) > s.config.MaxRoughtimeRadius {
        return ErrTimeDrift
    }

    return nil
}

// syncWithRoughtime refreshes Roughtime synchronization
func (s *Shard) syncWithRoughtime() error {
    startTime := time.Now()
    results := make([]roughtime.Result, 0, len(s.RoughtimeServers))
    
    for _, server := range s.RoughtimeServers {
        result, err := roughtime.QueryServer(server.Address, server.PublicKey)
        if err != nil {
            s.stats.RoughtimeFailures++
            continue
        }
        results = append(results, result)
    }

    if uint(len(results)) < s.config.MinRoughtimeResponses {
        return ErrInsufficientProofs
    }

    // Create new time proof
    proof := createRoughtimeProof(results, s.Location)
    s.timeProofs[uint64(len(s.timeProofs))] = proof
    
    // Update stats
    s.lastSync = time.Now()
    s.stats.LastSyncTime = s.lastSync
    s.stats.AvgSyncLatency = (s.stats.AvgSyncLatency + time.Since(startTime)) / 2
    s.stats.ProofCount++

    return nil
}

// findTargetShard determines the most appropriate shard for a timestamp
func (sm *ShardManager) findTargetShard(t time.Time) *Shard {
    sm.mutex.RLock()
    defer sm.mutex.RUnlock()

    var bestShard *Shard
    var minOffset time.Duration = time.Hour * 24

    for _, shard := range sm.shards {
        localTime := t.In(shard.Location)
        offset := abs(localTime.Sub(t))
        
        if offset < minOffset {
            bestShard = shard
            minOffset = offset
        }
    }

    return bestShard
}

// updateCrossShardRefs updates cross-shard references
func (sm *ShardManager) updateCrossShardRefs(sourceShard *Shard, entry *SequenceEntry) error {
    sm.mutex.RLock()
    defer sm.mutex.RUnlock()

    for _, targetShard := range sm.shards {
        if targetShard.ID == sourceShard.ID {
            continue
        }

        // Find related entries
        related := targetShard.findRelatedEntries(entry.VerifiedTime, sm.config.MaxShardDrift)
        for _, relatedEntry := range related {
            ref := CrossShardRef{
                SourceShard:    sourceShard.ID,
                TargetShard:    targetShard.ID,
                SourceSeqNum:   entry.SequenceNum,
                TargetSeqNum:   relatedEntry.SequenceNum,
                TimeDifference: relatedEntry.VerifiedTime.Sub(entry.VerifiedTime),
                Proof:          entry.TimeProof,
            }

            sourceShard.addCrossShardRef(ref)
            targetShard.addCrossShardRef(ref)
            
            sourceShard.stats.CrossShardRefs++
            targetShard.stats.CrossShardRefs++
        }
    }

    return nil
}

// findRelatedEntries finds entries within the time difference threshold
func (s *Shard) findRelatedEntries(t time.Time, maxDiff time.Duration) []*SequenceEntry {
    s.mutex.RLock()
    defer s.mutex.RUnlock()

    var related []*SequenceEntry
    for _, entry := range s.entries {
        if abs(entry.VerifiedTime.Sub(t)) <= maxDiff {
            related = append(related, entry)
        }
    }

    return related
}

// addCrossShardRef adds a cross-shard reference
func (s *Shard) addCrossShardRef(ref CrossShardRef) {
    s.mutex.Lock()
    defer s.mutex.Unlock()
    s.references[ref.TargetShard] = append(s.references[ref.TargetShard], ref)
}

// VerifyShardConsistency verifies consistency across all shards
func (sm *ShardManager) VerifyShardConsistency() error {
    sm.mutex.RLock()
    defer sm.mutex.RUnlock()

    // Verify each shard internally
    for _, shard := range sm.shards {
        if err := sm.verifier.VerifySequence(maps.Values(shard.entries)); err != nil {
            return fmt.Errorf("shard %s sequence verification failed: %w", shard.ID, err)
        }
    }

    // Verify cross-shard references if enabled
    if sm.config.EnableCrossShardValidation {
        if err := sm.verifyCrossShardRefs(); err != nil {
            return fmt.Errorf("cross-shard verification failed: %w", err)
        }
    }

    return nil
}

// verifyCrossShardRefs verifies all cross-shard references
func (sm *ShardManager) verifyCrossShardRefs() error {
    for _, sourceShard := range sm.shards {
        for targetID, refs := range sourceShard.references {
            targetShard, exists := sm.shards[targetID]
            if !exists {
                return fmt.Errorf("%w: %s", ErrShardNotFound, targetID)
            }

            for _, ref := range refs {
                if err := sm.verifyReference(ref, sourceShard, targetShard); err != nil {
                    return fmt.Errorf("invalid reference: %w", err)
                }
            }
        }
    }
    return nil
}

// verifyReference verifies a single cross-shard reference
func (sm *ShardManager) verifyReference(ref CrossShardRef, source, target *Shard) error {
    sourceEntry, exists := source.entries