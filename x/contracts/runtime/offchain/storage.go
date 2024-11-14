// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package offchain

import (
    "context"
    "encoding/binary"
    "errors"
    "fmt"
    "path/filepath"
    "sync"
    "time"

    "github.com/ava-labs/avalanchego/ids"
    "github.com/ava-labs/avalanchego/utils/logging"
    "github.com/ava-labs/hypersdk/codec"
    "github.com/syndtr/goleveldb/leveldb"
    "github.com/syndtr/goleveldb/leveldb/opt"
    "github.com/syndtr/goleveldb/leveldb/util"
)

var (
    ErrStorageClosed      = errors.New("storage is closed")
    ErrKeyNotFound        = errors.New("key not found")
    ErrInvalidNamespace   = errors.New("invalid namespace")
    ErrInvalidKey         = errors.New("invalid key")
    ErrValueTooLarge      = errors.New("value too large")
    ErrNamespaceTooLarge  = errors.New("namespace too large")
)

const (
    // Storage constants
    MaxKeySize        = 1024    // 1KB
    MaxValueSize      = 1 << 20 // 1MB
    MaxNamespaceSize  = 64      // 64B
    DefaultCacheSize  = 1 << 30 // 1GB
    
    // Prefix constants
    namespacePrefix  byte = 0x00
    metadataPrefix   byte = 0x01
    coordinationPrefix byte = 0x02 
    
    // Metadata keys
    lastGCKey        = "last_gc"
    versionKey       = "version"
)

// LocalStorage provides persistent storage for off-chain workers
type LocalStorage struct {
    db          *leveldb.DB
    cache       *StorageCache
    metrics     *StorageMetrics
    log         logging.Logger
    config      *StorageConfig

    // Namespace management
    namespaces  map[string]NamespaceInfo
    nsLock      sync.RWMutex

    // Lifecycle management
    ctx         context.Context
    cancel      context.CancelFunc
    wg          sync.WaitGroup
}

type StorageConfig struct {
    Path            string
    CacheSize       int
    MaxOpenFiles    int
    CompactionMode  opt.CompactionType
    GCInterval      time.Duration
    EnableMetrics   bool
}

type NamespaceInfo struct {
    Size      int64
    LastWrite time.Time
    KeyCount  int
}

type StorageMetrics struct {
    BytesWritten   int64
    BytesRead      int64
    KeysWritten    int64
    KeysRead       int64
    CacheHits      int64
    CacheMisses    int64
    lock           sync.Mutex
}

type StorageCache struct {
    entries    map[string]CacheEntry
    maxSize    int
    size       int
    lock       sync.RWMutex
}

type CacheEntry struct {
    value      []byte
    timestamp  time.Time
    size       int
}

// NewLocalStorage creates a new local storage instance
func NewLocalStorage(config *StorageConfig, log logging.Logger) (*LocalStorage, error) {
    // Open LevelDB
    opts := &opt.Options{
        BlockCacheCapacity:     config.CacheSize,
        OpenFilesCacheCapacity: config.MaxOpenFiles,
        CompactionTableSize:    32 * 1024 * 1024, // 32MB
        WriteBuffer:           16 * 1024 * 1024,  // 16MB
        CompactionType:        config.CompactionMode,
    }

    db, err := leveldb.OpenFile(config.Path, opts)
    if err != nil {
        return nil, fmt.Errorf("failed to open database: %w", err)
    }

    ctx, cancel := context.WithCancel(context.Background())

    ls := &LocalStorage{
        db:         db,
        log:        log,
        config:     config,
        namespaces: make(map[string]NamespaceInfo),
        cache: &StorageCache{
            entries: make(map[string]CacheEntry),
            maxSize: config.CacheSize,
        },
        metrics: &StorageMetrics{},
        ctx:     ctx,
        cancel:  cancel,
    }

    // Initialize storage
    if err := ls.initialize(); err != nil {
        db.Close()
        return nil, err
    }

    // Start background tasks
    ls.startBackgroundTasks()

    return ls, nil
}

// Initialize storage and load existing namespaces
func (ls *LocalStorage) initialize() error {
    // Check/create version
    version, err := ls.db.Get([]byte{metadataPrefix}, nil)
    if err != nil && !errors.Is(err, leveldb.ErrNotFound) {
        return fmt.Errorf("failed to read version: %w", err)
    }

    if errors.Is(err, leveldb.ErrNotFound) {
        // New database, set version
        if err := ls.db.Put([]byte{metadataPrefix}, []byte("1.0"), nil); err != nil {
            return fmt.Errorf("failed to set version: %w", err)
        }
    }

    // Load existing namespaces
    iter := ls.db.NewIterator(util.BytesPrefix([]byte{namespacePrefix}), nil)
    defer iter.Release()

    for iter.Next() {
        namespace := string(iter.Key()[1:])
        if err := ls.loadNamespaceInfo(namespace); err != nil {
            return fmt.Errorf("failed to load namespace %s: %w", namespace, err)
        }
    }

    return iter.Error()
}

// Put stores a value in the given namespace
func (ls *LocalStorage) Put(namespace string, key, value []byte) error {
    if err := ls.validateNamespace(namespace); err != nil {
        return err
    }

    if err := ls.validateKey(key); err != nil {
        return err
    }

    if err := ls.validateValue(value); err != nil {
        return err
    }

    dbKey := ls.makeKey(namespace, key)
    
    // Update storage
    if err := ls.db.Put(dbKey, value, nil); err != nil {
        return fmt.Errorf("failed to store value: %w", err)
    }

    // Update cache
    ls.updateCache(namespace, key, value)

    // Update namespace info
    ls.updateNamespaceInfo(namespace, len(value), 1)

    // Update metrics
    ls.updateMetrics(true, int64(len(value)), 1)

    return nil
}

// Get retrieves a value from the given namespace
func (ls *LocalStorage) Get(namespace string, key []byte) ([]byte, error) {
    if err := ls.validateNamespace(namespace); err != nil {
        return nil, err
    }

    if err := ls.validateKey(key); err != nil {
        return nil, err
    }

    // Check cache first
    if value, ok := ls.checkCache(namespace, key); ok {
        return value, nil
    }

    // Retrieve from storage
    dbKey := ls.makeKey(namespace, key)
    value, err := ls.db.Get(dbKey, nil)
    if err != nil {
        if errors.Is(err, leveldb.ErrNotFound) {
            return nil, ErrKeyNotFound
        }
        return nil, fmt.Errorf("failed to retrieve value: %w", err)
    }

    // Update cache
    ls.updateCache(namespace, key, value)

    // Update metrics
    ls.updateMetrics(false, int64(len(value)), 1)

    return value, nil
}

// Delete removes a value from the given namespace
func (ls *LocalStorage) Delete(namespace string, key []byte) error {
    if err := ls.validateNamespace(namespace); err != nil {
        return err
    }

    if err := ls.validateKey(key); err != nil {
        return err
    }

    dbKey := ls.makeKey(namespace, key)
    
    // Get current value size for namespace tracking
    value, err := ls.db.Get(dbKey, nil)
    size := 0
    if err == nil {
        size = len(value)
    }

    // Delete from storage
    if err := ls.db.Delete(dbKey, nil); err != nil {
        return fmt.Errorf("failed to delete value: %w", err)
    }

    // Remove from cache
    ls.removeFromCache(namespace, key)

    // Update namespace info
    ls.updateNamespaceInfo(namespace, -size, -1)

    return nil
}

// List returns all keys in the given namespace with optional prefix
func (ls *LocalStorage) List(namespace string, prefix []byte) ([][]byte, error) {
    if err := ls.validateNamespace(namespace); err != nil {
        return nil, err
    }

    var keys [][]byte
    iter := ls.db.NewIterator(util.BytesPrefix(ls.makeKey(namespace, prefix)), nil)
    defer iter.Release()

    for iter.Next() {
        key := iter.Key()[len(namespace)+1:] // Remove namespace prefix
        keys = append(keys, key)
    }

    if err := iter.Error(); err != nil {
        return nil, fmt.Errorf("failed to list keys: %w", err)
    }

    return keys, nil
}

// DeleteNamespace removes an entire namespace and its data
func (ls *LocalStorage) DeleteNamespace(namespace string) error {
    if err := ls.validateNamespace(namespace); err != nil {
        return err
    }

    batch := new(leveldb.Batch)
    
    // Delete all keys in namespace
    iter := ls.db.NewIterator(util.BytesPrefix(ls.makeKey(namespace, nil)), nil)
    defer iter.Release()

    for iter.Next() {
        batch.Delete(iter.Key())
    }

    if err := iter.Error(); err != nil {
        return fmt.Errorf("failed to iterate namespace: %w", err)
    }

    // Delete namespace metadata
    batch.Delete(ls.makeNamespaceKey(namespace))

    // Execute batch delete
    if err := ls.db.Write(batch, nil); err != nil {
        return fmt.Errorf("failed to delete namespace: %w", err)
    }

    // Clear namespace info
    ls.nsLock.Lock()
    delete(ls.namespaces, namespace)
    ls.nsLock.Unlock()

    // Clear cache entries for namespace
    ls.clearNamespaceCache(namespace)

    return nil
}

// Close closes the storage
func (ls *LocalStorage) Close() error {
    ls.cancel()
    ls.wg.Wait()
    return ls.db.Close()
}

// Internal methods

func (ls *LocalStorage) startBackgroundTasks() {
    // Start garbage collection
    ls.wg.Add(1)
    go ls.gcLoop()
}

func (ls *LocalStorage) gcLoop() {
    defer ls.wg.Done()

    ticker := time.NewTicker(ls.config.GCInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            if err := ls.runGC(); err != nil {
                ls.log.Error("Failed to run GC: %v", err)
            }
        case <-ls.ctx.Done():
            return
        }
    }
}

func (ls *LocalStorage) runGC() error {
    ls.cache.lock.Lock()
    defer ls.cache.lock.Unlock()

    // Clear expired cache entries
    now := time.Now()
    for key, entry := range ls.cache.entries {
        if now.Sub(entry.timestamp) > ls.config.GCInterval {
            delete(ls.cache.entries, key)
            ls.cache.size -= entry.size
        }
    }

    return nil
}

func (ls *LocalStorage) makeKey(namespace string, key []byte) []byte {
    // namespace + separator + key
    result := make([]byte, 0, len(namespace)+1+len(key))
    result = append(result, namespacePrefix)
    result = append(result, []byte(namespace)...)
    result = append(result, key...)
    return result
}

func (ls *LocalStorage) makeNamespaceKey(namespace string) []byte {
    return append([]byte{namespacePrefix}, []byte(namespace)...)
}

func (ls *LocalStorage) validateNamespace(namespace string) error {
    if len(namespace) == 0 || len(namespace) > MaxNamespaceSize {
        return ErrInvalidNamespace
    }
    return nil
}

func (ls *LocalStorage) validateKey(key []byte) error {
    if len(key) == 0 || len(key) > MaxKeySize {
        return ErrInvalidKey
    }
    return nil
}

func (ls *LocalStorage) validateValue(value []byte) error {
    if len(value) > MaxValueSize {
        return ErrValueTooLarge
    }
    return nil
}

func (ls *LocalStorage) updateCache(namespace string, key, value []byte) {
    ls.cache.lock.Lock()
    defer ls.cache.lock.Unlock()

    cacheKey := fmt.Sprintf("%s:%x", namespace, key)
    entry := CacheEntry{
        value:     value,
        timestamp: time.Now(),
        size:      len(value),
    }

    // Remove old entry if exists
    if old, exists := ls.cache.entries[cacheKey]; exists {
        ls.cache.size -= old.size
    }

    // Add new entry
    ls.cache.entries[cacheKey] = entry
    ls.cache.size += entry.size

    // Evict entries if cache is too large
    ls.evictCacheEntries()
}

func (ls *LocalStorage) checkCache(namespace string, key []byte) ([]byte, bool) {
    ls.cache.lock.RLock()
    defer ls.cache.lock.RUnlock()

    cacheKey := fmt.Sprintf("%s:%x", namespace, key)
    if entry, ok := ls.cache.entries[cacheKey]; ok {
        ls.metrics.lock.Lock()
        ls.metrics.CacheHits++
        ls.metrics.lock.Unlock()
        return entry.value, true
    }

    ls.metrics.lock.Lock()
    ls.metrics.CacheMisses++
    ls.metrics.lock.Unlock()
    return nil, false
}

func (ls *LocalStorage) evictCacheEntries() {
    for ls.cache.size > ls.cache.maxSize {
        // Find oldest entry
        var oldestKey string
        oldestTime := time.Now()
        
        for key, entry := range ls.cache.entries {
            if entry.timestamp.Before(oldestTime) {
                oldestKey = key
                oldestTime = entry.timestamp
            }
        }

        if oldestKey != "" {
            entry := ls.cache.entries[oldestKey]
            delete(ls.cache.entries, oldestKey)
            ls.cache.size -= entry.size
        }
    }
}

func (ls *LocalStorage) updateMetrics(isWrite bool, bytes, keys int64) {
    if !ls.config.EnableMetrics {
        return
    }

    ls.metrics.lock.Lock()
    defer ls.metrics.lock.Unlock()

    if isWrite {
        ls.metrics.BytesWritten += bytes
        ls.metrics.KeysWritten += keys
    } else {
        ls.metrics.BytesRead += bytes
        ls.metrics.KeysRead += keys
    }
}

func (ls *LocalStorage) GetMetrics() StorageMetrics {
    ls.metrics.lock.Lock()
    defer ls.metrics.lock.Unlock()
    return *ls.metrics
}

func (ls *LocalStorage) updateNamespaceInfo(namespace string, sizeChange int, keyChange int) {
    ls.nsLock.Lock()
    defer ls.nsLock.Unlock()

    info := ls.namespaces[namespace]
    info.Size += int64(sizeChange)
    info.KeyCount += keyChange
    info.LastWrite = time.Now()
    ls.namespaces[namespace] = info
}

func (ls *LocalStorage) loadNamespaceInfo(namespace string) error {
    key := ls.makeNamespaceKey(namespace)
    data, err := ls.db.Get(key, nil)
    if err != nil {
        if errors.Is(err, leveldb.ErrNotFound) {
            // Initialize new namespace
            ls.namespaces[namespace] = NamespaceInfo{
                LastWrite: time.Now(),
            }
            return nil
        }
        return err
    }

    var info NamespaceInfo
    if err := codec.Unmarshal(data, &info); err != nil {
        return fmt.Errorf("failed to unmarshal namespace info: %w", err)
    }

    ls.namespaces[namespace] = info
    return nil
}

func (ls *LocalStorage) saveNamespaceInfo(namespace string) error {
    ls.nsLock.RLock()
    info, exists := ls.namespaces[namespace]
    ls.nsLock.RUnlock()

    if !exists {
        return ErrInvalidNamespace
    }

    data, err := codec.Marshal(&info)
    if err != nil {
        return fmt.Errorf("failed to marshal namespace info: %w", err)
    }

    key := ls.makeNamespaceKey(namespace)
    if err := ls.db.Put(key, data, nil); err != nil {
        return fmt.Errorf("failed to save namespace info: %w", err)
    }

    return nil
}

func (ls *LocalStorage) removeFromCache(namespace string, key []byte) {
    ls.cache.lock.Lock()
    defer ls.cache.lock.Unlock()

    cacheKey := fmt.Sprintf("%s:%x", namespace, key)
    if entry, exists := ls.cache.entries[cacheKey]; exists {
        delete(ls.cache.entries, cacheKey)
        ls.cache.size -= entry.size
    }
}

func (ls *LocalStorage) clearNamespaceCache(namespace string) {
    ls.cache.lock.Lock()
    defer ls.cache.lock.Unlock()

    prefix := namespace + ":"
    for key, entry := range ls.cache.entries {
        if strings.HasPrefix(key, prefix) {
            delete(ls.cache.entries, key)
            ls.cache.size -= entry.size
        }
    }
}

// Batch operations

type Batch struct {
    namespace string
    batch     *leveldb.Batch
    size      int
}

// NewBatch creates a new batch for the given namespace
func (ls *LocalStorage) NewBatch(namespace string) (*Batch, error) {
    if err := ls.validateNamespace(namespace); err != nil {
        return nil, err
    }

    return &Batch{
        namespace: namespace,
        batch:     new(leveldb.Batch),
    }, nil
}

// Put adds a key-value pair to the batch
func (b *Batch) Put(key, value []byte) error {
    if len(key) == 0 || len(key) > MaxKeySize {
        return ErrInvalidKey
    }
    if len(value) > MaxValueSize {
        return ErrValueTooLarge
    }

    dbKey := append([]byte{namespacePrefix}, []byte(b.namespace)...)
    dbKey = append(dbKey, key...)
    b.batch.Put(dbKey, value)
    b.size += len(key) + len(value)

    return nil
}

// Delete adds a key deletion to the batch
func (b *Batch) Delete(key []byte) error {
    if len(key) == 0 || len(key) > MaxKeySize {
        return ErrInvalidKey
    }

    dbKey := append([]byte{namespacePrefix}, []byte(b.namespace)...)
    dbKey = append(dbKey, key...)
    b.batch.Delete(dbKey)

    return nil
}

// Write executes the batch operations
func (ls *LocalStorage) WriteBatch(b *Batch) error {
    if err := ls.validateNamespace(b.namespace); err != nil {
        return err
    }

    if err := ls.db.Write(b.batch, nil); err != nil {
        return fmt.Errorf("failed to write batch: %w", err)
    }

    // Update namespace info
    ls.updateNamespaceInfo(b.namespace, b.size, b.batch.Len())

    // Clear affected cache entries
    ls.clearNamespaceCache(b.namespace)

    return nil
}

// Iterator for range queries
type Iterator struct {
    iter      iterator.Iterator
    prefix    []byte
    namespace string
}

func (ls *LocalStorage) NewIterator(namespace string, start, limit []byte) (*Iterator, error) {
    if err := ls.validateNamespace(namespace); err != nil {
        return nil, err
    }

    prefix := ls.makeKey(namespace, nil)
    iter := ls.db.NewIterator(&util.Range{
        Start: append(prefix, start...),
        Limit: append(prefix, limit...),
    }, nil)

    return &Iterator{
        iter:      iter,
        prefix:    prefix,
        namespace: namespace,
    }, nil
}

func (it *Iterator) Next() bool {
    return it.iter.Next()
}

func (it *Iterator) Key() []byte {
    fullKey := it.iter.Key()
    return fullKey[len(it.prefix):]
}

func (it *Iterator) Value() []byte {
    return it.iter.Value()
}

func (it *Iterator) Release() {
    it.iter.Release()
}

func (it *Iterator) Error() error {
    return it.iter.Error()
}

// Helper functions for compound keys
func makeCompoundKey(parts ...[]byte) []byte {
    size := 0
    for _, part := range parts {
        size += len(part) + binary.MaxVarintLen64 // length prefix + data
    }

    key := make([]byte, 0, size)
    for _, part := range parts {
        var buf [binary.MaxVarintLen64]byte
        n := binary.PutUvarint(buf[:], uint64(len(part)))
        key = append(key, buf[:n]...)
        key = append(key, part...)
    }
    return key
}

func splitCompoundKey(key []byte) ([][]byte, error) {
    var parts [][]byte
    for len(key) > 0 {
        length, n := binary.Uvarint(key)
        if n <= 0 {
            return nil, errors.New("invalid compound key")
        }
        key = key[n:]
        if uint64(len(key)) < length {
            return nil, errors.New("invalid compound key")
        }
        parts = append(parts, key[:length])
        key = key[length:]
    }
    return parts, nil
}

// Coordination storage methods
type CoordinationState struct {
    WorkerID     WorkerID
    PartnerID    WorkerID
    TaskID       string
    Status       string
    LastUpdate   time.Time
    Metadata     []byte
}

// Store coordination state
func (ls *LocalStorage) StoreCoordinationState(state *CoordinationState) error {
    key := makeCoordinationKey(state.WorkerID, state.TaskID)
    data, err := codec.Marshal(state)
    if err != nil {
        return fmt.Errorf("failed to marshal coordination state: %w", err)
    }

    return ls.Put("coordination", key, data)
}

// Get coordination state
func (ls *LocalStorage) GetCoordinationState(workerID WorkerID, taskID string) (*CoordinationState, error) {
    key := makeCoordinationKey(workerID, taskID)
    data, err := ls.Get("coordination", key)
    if err != nil {
        return nil, err
    }

    var state CoordinationState
    if err := codec.Unmarshal(data, &state); err != nil {
        return nil, fmt.Errorf("failed to unmarshal coordination state: %w", err)
    }

    return &state, nil
}

// Helper for making coordination keys
func makeCoordinationKey(workerID WorkerID, taskID string) []byte {
    return makeCompoundKey(
        []byte(workerID),
        []byte(taskID),
    )
}

// List active coordinations for a worker
func (ls *LocalStorage) ListActiveCoordinations(workerID WorkerID) ([]*CoordinationState, error) {
    prefix := []byte(workerID)
    keys, err := ls.List("coordination", prefix)
    if err != nil {
        return nil, err
    }

    var states []*CoordinationState
    for _, key := range keys {
        state, err := ls.GetCoordinationState(workerID, string(key))
        if err != nil {
            continue
        }
        states = append(states, state)
    }

    return states, nil
}

// Delete coordination state
func (ls *LocalStorage) DeleteCoordinationState(workerID WorkerID, taskID string) error {
    key := makeCoordinationKey(workerID, taskID)
    return ls.Delete("coordination", key)
}