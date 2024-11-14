// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package offchain

import (
   "context"
   "errors"
   "fmt"
   "sync"
   "time"

   "github.com/ava-labs/avalanchego/ids"
   "github.com/ava-labs/hypersdk/chain"
   "github.com/ava-labs/hypersdk/codec"
   "github.com/ava-labs/hypersdk/state"
   // NEW: Add events package
   "github.com/ava-labs/hypersdk/runtime/events"
)

var (
   ErrReadOnlyState     = errors.New("attempted write operation on read-only state")
   ErrStateNotFound     = errors.New("state not found")
   ErrInvalidKey        = errors.New("invalid state key")
   ErrCacheMiss         = errors.New("state cache miss")
   ErrStateUnavailable  = errors.New("state unavailable")
)

// OffChainState manages state access for off-chain workers
type OffChainState struct {
   chainState  state.Mutable
   localState  state.Mutable
   cache       *StateCache
   blockHeight uint64
   blockHash   ids.ID
   
   // Access tracking
   readKeys    map[string]struct{}
   lock        sync.RWMutex

   // Metrics
   metrics     *StateMetrics

   // NEW: Event state tracking
   eventState   *events.EventState
}

// StateCache provides caching for frequently accessed state
type StateCache struct {
   entries     map[string]CacheEntry
   maxSize     int
   ttl         time.Duration
   lock        sync.RWMutex
}

type CacheEntry struct {
   value      []byte
   lastAccess time.Time
   blockHeight uint64
}

// StateMetrics tracks state access patterns
type StateMetrics struct {
   Reads          uint64
   CacheHits      uint64
   CacheMisses    uint64
   ReadLatency    time.Duration
   // NEW: Add event read tracking
   EventReads     uint64
   lock           sync.Mutex
}

// NewOffChainState creates a new off-chain state manager
func NewOffChainState(
   chainState state.Mutable,
   localState state.Mutable,
   cacheSize int,
   cacheTTL time.Duration,
) *OffChainState {
   return &OffChainState{
       chainState: chainState,
       localState: localState,
       cache: &StateCache{
           entries: make(map[string]CacheEntry),
           maxSize: cacheSize,
           ttl:     cacheTTL,
       },
       readKeys: make(map[string]struct{}),
       metrics:  &StateMetrics{},
       // NEW: Initialize event state
       eventState: events.NewEventState(),
   }
}

// GetValue retrieves a value from state
func (s *OffChainState) GetValue(ctx context.Context, key []byte) ([]byte, error) {
   if err := validateKey(key); err != nil {
       return nil, err
   }

   start := time.Now()
   defer s.updateReadMetrics(start)

   // Track read access
   s.trackRead(string(key))

   // Check cache first
   if value, ok := s.checkCache(key); ok {
       return value, nil
   }

   // Try local state first
   value, err := s.localState.GetValue(ctx, key)
   if err == nil {
       s.cacheValue(key, value)
       return value, nil
   }

   // Fall back to chain state
   value, err = s.chainState.GetValue(ctx, key)
   if err != nil {
       if errors.Is(err, state.ErrNotFound) {
           return nil, ErrStateNotFound
       }
       return nil, fmt.Errorf("%w: %v", ErrStateUnavailable, err)
   }

   s.cacheValue(key, value)
   return value, nil
}

// Insert prevents write operations in read-only state
func (s *OffChainState) Insert(_ context.Context, _ []byte, _ []byte) error {
   return ErrReadOnlyState
}

// Remove prevents write operations in read-only state
func (s *OffChainState) Remove(_ context.Context, _ []byte) error {
   return ErrReadOnlyState
}

// UpdateBlockContext updates the current block context
func (s *OffChainState) UpdateBlockContext(height uint64, hash ids.ID) {
   s.lock.Lock()
   defer s.lock.Unlock()

   s.blockHeight = height
   s.blockHash = hash

   // Clear invalid cache entries
   s.cache.clearOutdated(height)
}

// GetReadSet returns the set of keys read during execution
func (s *OffChainState) GetReadSet() [][]byte {
   s.lock.RLock()
   defer s.lock.RUnlock()

   keys := make([][]byte, 0, len(s.readKeys))
   for key := range s.readKeys {
       keys = append(keys, []byte(key))
   }
   return keys
}

// GetMetrics returns current state access metrics
func (s *OffChainState) GetMetrics() StateMetrics {
   s.metrics.lock.Lock()
   defer s.metrics.lock.Unlock()
   return *s.metrics
}

// NEW: Add GetEventState method
func (s *OffChainState) GetEventState() *events.EventState {
   return s.eventState
}

// NEW: Add trackEventAccess method
func (s *OffChainState) trackEventAccess(evt events.Event) {
   s.lock.Lock()
   defer s.lock.Unlock()
   
   // Track event data access
   key := fmt.Sprintf("event:%s:%x", evt.EventType, evt.Data)
   s.readKeys[key] = struct{}{}

   // Update metrics
   s.metrics.lock.Lock()
   s.metrics.EventReads++
   s.metrics.lock.Unlock()
}

// Internal helper methods

func (s *OffChainState) trackRead(key string) {
   s.lock.Lock()
   defer s.lock.Unlock()
   s.readKeys[key] = struct{}{}
}

func (s *OffChainState) checkCache(key []byte) ([]byte, bool) {
   s.cache.lock.RLock()
   defer s.cache.lock.RUnlock()

   entry, exists := s.cache.entries[string(key)]
   if !exists {
       s.updateCacheMiss()
       return nil, false
   }

   if time.Since(entry.lastAccess) > s.cache.ttl {
       s.updateCacheMiss()
       return nil, false
   }

   if entry.blockHeight != s.blockHeight {
       s.updateCacheMiss()
       return nil, false
   }

   s.updateCacheHit()
   return entry.value, true
}

func (s *OffChainState) cacheValue(key []byte, value []byte) {
   s.cache.lock.Lock()
   defer s.cache.lock.Unlock()

   // Enforce cache size limit
   if len(s.cache.entries) >= s.cache.maxSize {
       s.cache.evictOldest()
   }

   s.cache.entries[string(key)] = CacheEntry{
       value:       value,
       lastAccess:  time.Now(),
       blockHeight: s.blockHeight,
   }
}

func (s *OffChainState) updateReadMetrics(start time.Time) {
   s.metrics.lock.Lock()
   defer s.metrics.lock.Unlock()

   s.metrics.Reads++
   s.metrics.ReadLatency += time.Since(start)
}

func (s *OffChainState) updateCacheHit() {
   s.metrics.lock.Lock()
   defer s.metrics.lock.Unlock()
   s.metrics.CacheHits++
}

func (s *OffChainState) updateCacheMiss() {
   s.metrics.lock.Lock()
   defer s.metrics.lock.Unlock()
   s.metrics.CacheMisses++
}

// Cache management methods

func (c *StateCache) clearOutdated(currentHeight uint64) {
   c.lock.Lock()
   defer c.lock.Unlock()

   for key, entry := range c.entries {
       if entry.blockHeight != currentHeight {
           delete(c.entries, key)
       }
   }
}

func (c *StateCache) evictOldest() {
   var oldestKey string
   var oldestTime time.Time

   // Find oldest entry
   first := true
   for key, entry := range c.entries {
       if first || entry.lastAccess.Before(oldestTime) {
           oldestKey = key
           oldestTime = entry.lastAccess
           first = false
       }
   }

   // Remove oldest entry
   if oldestKey != "" {
       delete(c.entries, oldestKey)
   }
}

// Validation helpers

func validateKey(key []byte) error {
   if len(key) == 0 {
       return ErrInvalidKey
   }
   return nil
}

// ReadOnlyStateSnapshot represents a point-in-time view of state
type ReadOnlyStateSnapshot struct {
   height uint64
   hash   ids.ID
   state  *OffChainState
}

// NewReadOnlyStateSnapshot creates a new state snapshot
func NewReadOnlyStateSnapshot(
   height uint64,
   hash ids.ID,
   state *OffChainState,
) *ReadOnlyStateSnapshot {
   return &ReadOnlyStateSnapshot{
       height: height,
       hash:   hash,
       state:  state,
   }
}

func (s *ReadOnlyStateSnapshot) GetValue(ctx context.Context, key []byte) ([]byte, error) {
   // Verify snapshot is still valid
   if s.height != s.state.blockHeight {
       return nil, fmt.Errorf("snapshot expired: current height %d, snapshot height %d", 
           s.state.blockHeight, s.height)
   }

   return s.state.GetValue(ctx, key)
}

// StateIterator allows iteration over state entries
type StateIterator struct {
   prefix []byte
   state  *OffChainState
   keys   [][]byte
   index  int
}

// NewStateIterator creates an iterator over state with given prefix
func NewStateIterator(state *OffChainState, prefix []byte) *StateIterator {
   // Get all keys with prefix
   keys := make([][]byte, 0)
   for key := range state.readKeys {
       if hasPrefix([]byte(key), prefix) {
           keys = append(keys, []byte(key))
       }
   }

   return &StateIterator{
       prefix: prefix,
       state:  state,
       keys:   keys,
       index:  0,
   }
}

func (it *StateIterator) Next() bool {
   it.index++
   return it.index < len(it.keys)
}

func (it *StateIterator) Key() []byte {
   if it.index >= len(it.keys) {
       return nil
   }
   return it.keys[it.index]
}

func (it *StateIterator) Value() ([]byte, error) {
   if it.index >= len(it.keys) {
       return nil, ErrStateNotFound
   }
   return it.state.GetValue(context.Background(), it.keys[it.index])
}

func hasPrefix(key, prefix []byte) bool {
   if len(key) < len(prefix) {
       return false
   }
   for i := range prefix {
       if key[i] != prefix[i] {
           return false
       }
   }
   return true
}