// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timechain

import (
    "context"
    "errors"
    "fmt"
    "sync"
    "time"

    "golang.org/x/crypto/sha3"
)

var (
    // Existing errors
    ErrWindowFull        = errors.New("time window is full")
    ErrOutsideWindow     = errors.New("entry outside current window")
    ErrInvalidPrevHash   = errors.New("invalid previous hash")
    ErrSequenceGap       = errors.New("sequence number gap detected")
    ErrWindowOverlap     = errors.New("window overlap detected")

    // NEW: Additional errors for window management
    ErrWindowTransitionFailed = errors.New("window transition failed")
    ErrEntryOrderViolation   = errors.New("entry order violation across windows")
    ErrWindowGapDetected     = errors.New("gap detected between windows")
    ErrWindowNotReady        = errors.New("window not ready for entries")
)

// NEW: Add these new types
type WindowTransition struct {
    OldWindow     *TimeWindow
    NewWindow     *TimeWindow
    TransitionTime time.Time
    EntryCount    int
}

type WindowGap struct {
    StartTime time.Time
    EndTime   time.Time
    PrevHash  []byte
    Detected  time.Time
}

type TimeSequence struct {
    // Existing fields
    roughtime      *RoughtimeClient
    verifier       *Verifier
    windows        []*TimeWindow
    currentWindow  *TimeWindow
    lastEntry      *SequenceEntry
    nextSeqNum     uint64
    windowDuration time.Duration
    maxWindows     int
    mutex          sync.RWMutex
	batchGenerator *ProofBatchGenerator

    // Existing lifecycle fields
    ctx            context.Context
    cancel         context.CancelFunc
    wg             sync.WaitGroup
    isRunning      bool
    config         *SequenceConfig

    // NEW: Window synchronization fields
    transitions     []*WindowTransition
    detectedGaps    []*WindowGap
    pendingEntries  []*SequenceEntry
    transitionLock  sync.RWMutex

	// Chain coordination
    blockSync    *BlockSync
    lastBlock    uint64
}

// Your existing SequenceConfig remains the same
type SequenceConfig struct {
    MinEntriesPerWindow   uint64
    MaxEntriesPerWindow   uint64
    PruneThreshold        uint64
    RotationCheckInterval time.Duration
}

// Your existing NewTimeSequence remains the same
func NewTimeSequence(
    roughtime *RoughtimeClient,
    windowDuration time.Duration,
    verifier *Verifier,
    maxWindows int,
    config *SequenceConfig,
) *TimeSequence {
    if config == nil {
        config = &SequenceConfig{
            MinEntriesPerWindow:   1,
            MaxEntriesPerWindow:   1000,
            PruneThreshold:        uint64(maxWindows),
            RotationCheckInterval: windowDuration / 4,
        }
    }

    ts := &TimeSequence{
        // Core components
        roughtime:      roughtime,
        verifier:       verifier,
        windows:        make([]*TimeWindow, 0),
        windowDuration: windowDuration,
        maxWindows:     maxWindows,
        nextSeqNum:     0,
        config:         config,

        // Window management
        transitions:    make([]*WindowTransition, 0),
        detectedGaps:   make([]*WindowGap, 0),
        pendingEntries: make([]*SequenceEntry, 0),

        // Synchronization primitives
        mutex:          sync.RWMutex{},
        transitionLock: sync.RWMutex{},

        // Sequence state
        isRunning:     false,
        lastEntry:     nil,
        currentWindow: nil,
    }

    // Initialize batch generator with default worker count
    ts.batchGenerator = NewProofBatchGenerator(ts, 4)

    // Initialize context (will be properly set in Start())
    ts.ctx, ts.cancel = context.WithCancel(context.Background())

    return ts
}

// MODIFIED: Enhanced AddEntry with window synchronization
func (ts *TimeSequence) AddEntry(entry *SequenceEntry) error {
    ts.mutex.Lock()
    defer ts.mutex.Unlock()

    if !ts.isRunning {
        return errors.New("sequence not running")
    }

    // Validate entry
    if err := ts.validateEntry(entry); err != nil {
        return fmt.Errorf("entry validation failed: %w", err)
    }

    // Check if entry belongs in current window
    if ts.currentWindow != nil {
        if entry.VerifiedTime.After(ts.currentWindow.EndTime) {
            // Entry belongs in a future window
            ts.pendingEntries = append(ts.pendingEntries, entry)
            // Trigger window rotation if needed
            if err := ts.ensureCurrentWindow(entry.VerifiedTime); err != nil {
                return fmt.Errorf("window management failed: %w", err)
            }
            return nil
        }
    }

    // Ensure we have a current window
    if err := ts.ensureCurrentWindow(entry.VerifiedTime); err != nil {
        return fmt.Errorf("window management failed: %w", err)
    }

    // Add to current window
    if err := ts.currentWindow.AddEntry(entry); err != nil {
        return fmt.Errorf("failed to add to window: %w", err)
    }

    // Update sequence state
    ts.lastEntry = entry
    ts.nextSeqNum = entry.SequenceNum + 1

    return nil
}

// validateEntry performs comprehensive entry validation
func (ts *TimeSequence) validateEntry(entry *SequenceEntry) error {
    // Verify sequence number
    if entry.SequenceNum != ts.nextSeqNum {
        return fmt.Errorf("%w: expected %d, got %d", 
            ErrSequenceGap, ts.nextSeqNum, entry.SequenceNum)
    }

    // Verify previous hash if not first entry
    if ts.lastEntry != nil {
        expectedHash := calculateEntryHash(ts.lastEntry)
        if !entry.PrevHash.Equals(expectedHash) {
            return ErrInvalidPrevHash
        }
    }

    // Verify time proof
    if err := ts.verifier.VerifyTimeProof(entry.TimeProof); err != nil {
        return fmt.Errorf("proof verification failed: %w", err)
    }

    return nil
}

// ensureCurrentWindow manages time windows
func (ts *TimeSequence) ensureCurrentWindow(timestamp time.Time) error {
    // Check if we need a new window
    if ts.currentWindow == nil || timestamp.After(ts.currentWindow.EndTime) {
        if err := ts.createNewWindow(timestamp); err != nil {
            return err
        }
    }

    // Verify timestamp fits in current window
    if timestamp.Before(ts.currentWindow.StartTime) || 
       timestamp.After(ts.currentWindow.EndTime) {
        return ErrOutsideWindow
    }

    return nil
}

func (ts *TimeSequence) AddEntry(entry *SequenceEntry) error {
    ts.mutex.Lock()
    defer ts.mutex.Unlock()

    if !ts.isRunning {
        return errors.New("sequence not running")
    }

    // Validate entry
    if err := ts.validateEntry(entry); err != nil {
        return fmt.Errorf("entry validation failed: %w", err)
    }

    // Check if entry belongs in current window
    if ts.currentWindow != nil {
        if entry.VerifiedTime.After(ts.currentWindow.EndTime) {
            // Entry belongs in a future window
            ts.pendingEntries = append(ts.pendingEntries, entry)
            // Trigger window rotation if needed
            if err := ts.ensureCurrentWindow(entry.VerifiedTime); err != nil {
                return fmt.Errorf("window management failed: %w", err)
            }
            return nil
        }
    }

    // Ensure we have a current window
    if err := ts.ensureCurrentWindow(entry.VerifiedTime); err != nil {
        return fmt.Errorf("window management failed: %w", err)
    }

    // Add to current window
    if err := ts.currentWindow.AddEntry(entry); err != nil {
        return fmt.Errorf("failed to add to window: %w", err)
    }

    // Update sequence state
    ts.lastEntry = entry
    ts.nextSeqNum = entry.SequenceNum + 1

    return nil
}

// Your existing validateEntry remains the same

// MODIFIED: Enhanced createNewWindow with transition handling
func (ts *TimeSequence) createNewWindow(timestamp time.Time) error {
    ts.transitionLock.Lock()
    defer ts.transitionLock.Unlock()

    // Round timestamp to window boundary
    windowStart := timestamp.Truncate(ts.windowDuration)
    
    // Verify no gap with previous window
    if ts.currentWindow != nil {
        if !windowStart.Equal(ts.currentWindow.EndTime) {
            gap := &WindowGap{
                StartTime: ts.currentWindow.EndTime,
                EndTime:   windowStart,
                PrevHash:  ts.currentWindow.GetRoot(),
                Detected:  time.Now(),
            }
            ts.detectedGaps = append(ts.detectedGaps, gap)
            return fmt.Errorf("%w: gap between %v and %v", 
                ErrWindowGapDetected, ts.currentWindow.EndTime, windowStart)
        }
    }

    // Create new window
    newWindow := &TimeWindow{
        StartTime:       windowStart,
        EndTime:         windowStart.Add(ts.windowDuration),
        Entries:         make([]*SequenceEntry, 0),
        RoughtimeProofs: make([]*RoughtimeProof, 0),
    }

    // Link to previous window
    if ts.currentWindow != nil {
        newWindow.PrevWindowHash = ts.currentWindow.GetRoot()
        
        // Record transition
        transition := &WindowTransition{
            OldWindow:     ts.currentWindow,
            NewWindow:     newWindow,
            TransitionTime: time.Now(),
            EntryCount:    len(ts.currentWindow.Entries),
        }
        ts.transitions = append(ts.transitions, transition)
    }

    // Process any pending entries that belong in the new window
    if err := ts.processPendingEntries(newWindow); err != nil {
        return fmt.Errorf("failed to process pending entries: %w", err)
    }

    // Add to windows slice
    ts.windows = append(ts.windows, newWindow)

    // Prune old windows if needed
    if len(ts.windows) > ts.maxWindows {
        ts.pruneOldWindows()
    }

    ts.currentWindow = newWindow
    return nil
}

// NEW: Add pending entry handling
func (ts *TimeSequence) processPendingEntries(window *TimeWindow) error {
    var remainingEntries []*SequenceEntry
    
    for _, entry := range ts.pendingEntries {
        if entry.VerifiedTime.Before(window.EndTime) && 
           entry.VerifiedTime.After(window.StartTime) {
            // Entry belongs in this window
            if err := window.AddEntry(entry); err != nil {
                remainingEntries = append(remainingEntries, entry)
                continue
            }
        } else {
            // Entry belongs in a future window
            remainingEntries = append(remainingEntries, entry)
        }
    }
    
    ts.pendingEntries = remainingEntries
    return nil
}

// NEW: Add window gap handling
func (ts *TimeSequence) handleWindowGap(gap *WindowGap) error {
    intermediateStart := gap.StartTime
    for intermediateStart.Before(gap.EndTime) {
        intermediateWindow := &TimeWindow{
            StartTime:      intermediateStart,
            EndTime:        intermediateStart.Add(ts.windowDuration),
            PrevWindowHash: gap.PrevHash,
        }
        
        ts.windows = append(ts.windows, intermediateWindow)
        
        intermediateStart = intermediateStart.Add(ts.windowDuration)
        gap.PrevHash = intermediateWindow.GetRoot()
    }
    
    return nil
}

// NEW: Add methods to query window state
func (ts *TimeSequence) GetWindowTransitions() []*WindowTransition {
    ts.transitionLock.RLock()
    defer ts.transitionLock.RUnlock()
    
    transitions := make([]*WindowTransition, len(ts.transitions))
    copy(transitions, ts.transitions)
    return transitions
}

func (ts *TimeSequence) GetDetectedGaps() []*WindowGap {
    ts.transitionLock.RLock()
    defer ts.transitionLock.RUnlock()
    
    gaps := make([]*WindowGap, len(ts.detectedGaps))
    copy(gaps, ts.detectedGaps)
    return gaps
}

func (ts *TimeSequence) GetPendingEntryCount() int {
    ts.mutex.RLock()
    defer ts.mutex.RUnlock()
    return len(ts.pendingEntries)
}

// NEW: Add validation for window transitions
func (ts *TimeSequence) validateWindowTransition(old, new *TimeWindow) error {
    if old == nil {
        return nil // First window
    }

    // Verify time continuity
    if !new.StartTime.Equal(old.EndTime) {
        return ErrWindowGapDetected
    }

    // Verify hash chain
    if !bytes.Equal(new.PrevWindowHash, old.GetRoot()) {
        return ErrInvalidPrevHash
    }

    return nil
}

// pruneOldWindows removes oldest windows when limit is reached
func (ts *TimeSequence) pruneOldWindows() {
    excess := len(ts.windows) - ts.maxWindows
    if excess > 0 {
        // Keep the most recent windows
        ts.windows = ts.windows[excess:]
    }
}

// GetWindow returns a specific time window
func (ts *TimeSequence) GetWindow(startTime time.Time) *TimeWindow {
    ts.mutex.RLock()
    defer ts.mutex.RUnlock()

    for _, window := range ts.windows {
        if window.StartTime.Equal(startTime) {
            return window
        }
    }
    return nil
}

// GetCurrentWindow returns the current time window
func (ts *TimeSequence) GetCurrentWindow() *TimeWindow {
    ts.mutex.RLock()
    defer ts.mutex.RUnlock()
    return ts.currentWindow
}

// GetSequenceEntry returns an entry by sequence number
func (ts *TimeSequence) GetSequenceEntry(seqNum uint64) *SequenceEntry {
    ts.mutex.RLock()
    defer ts.mutex.RUnlock()

    for _, window := range ts.windows {
        for _, entry := range window.Entries {
            if entry.SequenceNum == seqNum {
                return entry
            }
        }
    }
    return nil
}

// GetNextSequenceNum returns the next sequence number
func (ts *TimeSequence) GetNextSequenceNum() uint64 {
    ts.mutex.RLock()
    defer ts.mutex.RUnlock()
    return ts.nextSeqNum
}

// GetWindowRange returns entries within a time range
func (ts *TimeSequence) GetWindowRange(start, end time.Time) []*SequenceEntry {
    ts.mutex.RLock()
    defer ts.mutex.RUnlock()

    var entries []*SequenceEntry
    for _, window := range ts.windows {
        // Skip windows outside range
        if window.EndTime.Before(start) || window.StartTime.After(end) {
            continue
        }

        // Add entries within range
        for _, entry := range window.Entries {
            if entry.VerifiedTime.After(start) && 
               entry.VerifiedTime.Before(end) {
                entries = append(entries, entry)
            }
        }
    }

    return entries
}

// VerifyWindowSequence verifies sequence integrity within a window
func (ts *TimeSequence) VerifyWindowSequence(window *TimeWindow) error {
    if window == nil {
        return errors.New("nil window")
    }

    return ts.verifier.VerifySequence(window.Entries)
}

// GetProof generates a proof for a specific sequence entry
func (ts *TimeSequence) GetProof(seqNum uint64) (*SequenceProof, error) {
    ts.mutex.RLock()
    defer ts.mutex.RUnlock()

    entry := ts.GetSequenceEntry(seqNum)
    if entry == nil {
        return nil, errors.New("entry not found")
    }

    // Find containing window
    var window *TimeWindow
    for _, w := range ts.windows {
        if containsEntry(w, entry) {
            window = w
            break
        }
    }

    if window == nil {
        return nil, errors.New("window not found")
    }

    // Generate proof
    return &SequenceProof{
        Entry:         entry,
        WindowRoot:    window.GetRoot(),
        WindowBounds:  [2]time.Time{window.StartTime, window.EndTime},
        ProofPath:    generateProofPath(window, entry),
    }, nil
}

// OnBlockCommit handles new block commits
func (ts *TimeSequence) OnBlockCommit(height uint64, blockID ids.ID, timestamp time.Time) error {
    if ts.blockSync == nil {
        ts.blockSync = NewBlockSync(ts)
    }
    return ts.blockSync.OnBlockCommit(height, blockID, timestamp)
}

// OnBlockReorg handles chain reorganizations
func (ts *TimeSequence) OnBlockReorg(height uint64) error {
    if ts.blockSync == nil {
        return errors.New("block sync not initialized")
    }
    return ts.blockSync.OnBlockReorg(height)
}

// Helper function to check if window contains entry
func containsEntry(window *TimeWindow, entry *SequenceEntry) bool {
    for _, e := range window.Entries {
        if e.SequenceNum == entry.SequenceNum {
            return true
        }
    }
    return false
}

// Start begins sequence lifecycle management
func (ts *TimeSequence) Start() error {
    ts.mutex.Lock()
    defer ts.mutex.Unlock()

    if ts.isRunning {
        return errors.New("sequence already running")
    }

    ts.ctx, ts.cancel = context.WithCancel(context.Background())
    ts.isRunning = true

    // Start background window management
    ts.wg.Add(1)
    go ts.windowManagementLoop()

    return nil
}

// Stop gracefully shuts down the sequence
func (ts *TimeSequence) Stop() error {
    ts.mutex.Lock()
    if !ts.isRunning {
        ts.mutex.Unlock()
        return nil
    }
    ts.isRunning = false
    ts.mutex.Unlock()

    ts.cancel()
    ts.wg.Wait()

    // Clean up batch generator resources if needed
    if ts.batchGenerator != nil {
        // Any cleanup needed for batch generator
    }

    return nil
}

// windowManagementLoop handles automatic window rotation
func (ts *TimeSequence) windowManagementLoop() {
    defer ts.wg.Done()

    ticker := time.NewTicker(ts.config.RotationCheckInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            ts.checkWindowRotation()
        case <-ts.ctx.Done():
            return
        }
    }
}

// checkWindowRotation checks if window rotation is needed
func (ts *TimeSequence) checkWindowRotation() {
    ts.mutex.Lock()
    defer ts.mutex.Unlock()

    now := time.Now()

    // Check if current window needs rotation
    if ts.currentWindow != nil {
        if now.After(ts.currentWindow.EndTime) ||
           len(ts.currentWindow.Entries) >= int(ts.config.MaxEntriesPerWindow) {
            if err := ts.createNewWindow(now); err != nil {
                // Log error but continue
                return
            }
        }
    } else {
        // Initialize first window if needed
        if err := ts.createNewWindow(now); err != nil {
            // Log error but continue
            return
        }
    }
}

// createCheckpoint creates a persistence point
func (ts *TimeSequence) createCheckpoint() error {
    ts.persistLock.Lock()
    defer ts.persistLock.Unlock()

    if !ts.config.PersistenceEnabled || ts.db == nil {
        return nil
    }

    state := &SequenceState{
        NextSeqNum: ts.nextSeqNum,
        LastEntry:  ts.lastEntry,
        Windows:    ts.windows,
    }

    bytes, err := codec.Marshal(state)
    if err != nil {
        return err
    }

    if err := ts.db.Put([]byte("sequence_state"), bytes); err != nil {
        return err
    }

    ts.lastCheckpoint = ts.nextSeqNum
    return nil
}

// recover attempts to restore from last known good state
func (ts *TimeSequence) recover() error {
    ts.recoveryLock.Lock()
    defer ts.recoveryLock.Unlock()

    if !ts.config.RecoveryEnabled || ts.db == nil {
        return nil
    }

    ts.recoveryMode = true
    defer func() { ts.recoveryMode = false }()

    // Load last state
    bytes, err := ts.db.Get([]byte("sequence_state"))
    if err != nil {
        if errors.Is(err, database.ErrNotFound) {
            return nil // Fresh start
        }
        return err
    }

    var state SequenceState
    if err := codec.Unmarshal(bytes, &state); err != nil {
        return err
    }

    // Verify state before applying
    if err := ts.verifyStateIntegrity(&state); err != nil {
        return err
    }

    // Apply state
    ts.nextSeqNum = state.NextSeqNum
    ts.lastEntry = state.LastEntry
    ts.windows = state.Windows
    if len(ts.windows) > 0 {
        ts.currentWindow = ts.windows[len(ts.windows)-1]
    }

    return nil
}

// Add checkpointing to your existing Start method
func (ts *TimeSequence) Start() error {
    ts.mutex.Lock()
    defer ts.mutex.Unlock()

    if ts.isRunning {
        return errors.New("sequence already running")
    }

    // Try recovery if enabled
    if ts.config.RecoveryEnabled {
        if err := ts.recover(); err != nil {
            return fmt.Errorf("recovery failed: %w", err)
        }
    }

    ts.ctx, ts.cancel = context.WithCancel(context.Background())
    ts.isRunning = true

    // Start background tasks
    ts.wg.Add(2)
    go ts.windowManagementLoop()
    go ts.checkpointLoop() // New loop for checkpoints

    return nil
}

// Add new checkpoint loop
func (ts *TimeSequence) checkpointLoop() {
    defer ts.wg.Done()

    if !ts.config.PersistenceEnabled {
        return
    }

    ticker := time.NewTicker(ts.config.CheckpointInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            if err := ts.createCheckpoint(); err != nil {
                // Log error but continue
                continue
            }
        case <-ts.ctx.Done():
            return
        }
    }
}