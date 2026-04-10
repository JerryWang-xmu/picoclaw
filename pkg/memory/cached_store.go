package memory

import (
	"container/list"
	"context"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/providers"
)

// cacheEntry represents a single cached session history entry
type cacheEntry struct {
	key       string
	value     []providers.Message
	timestamp time.Time
	element   *list.Element
}

// lruCache implements a thread-safe LRU cache with TTL support
type lruCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
	lruList *list.List
	maxSize int
	ttl     time.Duration
}

// newLRUCache creates a new LRU cache with the specified size and TTL
func newLRUCache(maxSize int, ttl time.Duration) *lruCache {
	return &lruCache{
		entries: make(map[string]*cacheEntry),
		lruList: list.New(),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

// get retrieves a value from the cache if it exists and hasn't expired
func (c *lruCache) get(key string) ([]providers.Message, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, exists := c.entries[key]
	if !exists {
		return nil, false
	}

	// Check if entry has expired
	if time.Since(entry.timestamp) > c.ttl {
		c.removeEntry(entry)
		return nil, false
	}

	// Move to front (most recently used)
	c.lruList.MoveToFront(entry.element)

	// Return a copy to prevent external modification
	result := make([]providers.Message, len(entry.value))
	copy(result, entry.value)
	return result, true
}

// set adds or updates a value in the cache
func (c *lruCache) set(key string, value []providers.Message) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If key already exists, update it and move to front
	if entry, exists := c.entries[key]; exists {
		entry.value = make([]providers.Message, len(value))
		copy(entry.value, value)
		entry.timestamp = time.Now()
		c.lruList.MoveToFront(entry.element)
		return
	}

	// Evict oldest entries if at capacity
	for len(c.entries) >= c.maxSize {
		oldest := c.lruList.Back()
		if oldest == nil {
			break
		}
		c.removeEntry(oldest.Value.(*cacheEntry))
	}

	// Create new entry
	entry := &cacheEntry{
		key:       key,
		value:     make([]providers.Message, len(value)),
		timestamp: time.Now(),
	}
	copy(entry.value, value)
	entry.element = c.lruList.PushFront(entry)
	c.entries[key] = entry
}

// invalidate removes a specific key from the cache
func (c *lruCache) invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, exists := c.entries[key]; exists {
		c.removeEntry(entry)
	}
}

// removeEntry removes an entry from both the map and the LRU list
// Must be called with write lock held
func (c *lruCache) removeEntry(entry *cacheEntry) {
	c.lruList.Remove(entry.element)
	delete(c.entries, entry.key)
}

// clear removes all entries from the cache
func (c *lruCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*cacheEntry)
	c.lruList.Init()
}

// CachedStore wraps a Store with an LRU cache for GetHistory operations
type CachedStore struct {
	underlying Store
	cache      *lruCache
}

// NewCachedStore creates a new cached store wrapper
func NewCachedStore(underlying Store, ttl time.Duration, maxSize int) *CachedStore {
	return &CachedStore{
		underlying: underlying,
		cache:      newLRUCache(maxSize, ttl),
	}
}

// AddMessage appends a message and updates the cache with the new history
func (c *CachedStore) AddMessage(ctx context.Context, sessionKey, role, content string) error {
	// First, get current history (from cache if available)
	history, err := c.GetHistory(ctx, sessionKey)
	if err != nil {
		return err
	}

	// Append new message to underlying store
	if err := c.underlying.AddMessage(ctx, sessionKey, role, content); err != nil {
		return err
	}

	// Update cache with new history
	newMsg := providers.Message{Role: role, Content: content}
	history = append(history, newMsg)
	c.cache.set(sessionKey, history)

	return nil
}

// AddFullMessage appends a full message and updates the cache with the new history
func (c *CachedStore) AddFullMessage(ctx context.Context, sessionKey string, msg providers.Message) error {
	// First, get current history (from cache if available)
	history, err := c.GetHistory(ctx, sessionKey)
	if err != nil {
		return err
	}

	// Append new message to underlying store
	if err := c.underlying.AddFullMessage(ctx, sessionKey, msg); err != nil {
		return err
	}

	// Update cache with new history
	history = append(history, msg)
	c.cache.set(sessionKey, history)

	return nil
}

// GetHistory returns cached history if available, otherwise fetches from underlying store
func (c *CachedStore) GetHistory(ctx context.Context, sessionKey string) ([]providers.Message, error) {
	// Try cache first
	if cached, hit := c.cache.get(sessionKey); hit {
		return cached, nil
	}

	// Cache miss - fetch from underlying store
	history, err := c.underlying.GetHistory(ctx, sessionKey)
	if err != nil {
		return nil, err
	}

	// Store in cache
	c.cache.set(sessionKey, history)

	return history, nil
}

// GetSummary delegates to underlying store (not cached)
func (c *CachedStore) GetSummary(ctx context.Context, sessionKey string) (string, error) {
	return c.underlying.GetSummary(ctx, sessionKey)
}

// SetSummary updates summary and invalidates cache
func (c *CachedStore) SetSummary(ctx context.Context, sessionKey, summary string) error {
	return c.underlying.SetSummary(ctx, sessionKey, summary)
}

// TruncateHistory truncates history and invalidates cache
func (c *CachedStore) TruncateHistory(ctx context.Context, sessionKey string, keepLast int) error {
	err := c.underlying.TruncateHistory(ctx, sessionKey, keepLast)
	if err != nil {
		return err
	}
	c.cache.invalidate(sessionKey)
	return nil
}

// SetHistory replaces history and invalidates cache
func (c *CachedStore) SetHistory(ctx context.Context, sessionKey string, history []providers.Message) error {
	err := c.underlying.SetHistory(ctx, sessionKey, history)
	if err != nil {
		return err
	}
	c.cache.invalidate(sessionKey)
	return nil
}

// Compact compacts storage and invalidates cache
func (c *CachedStore) Compact(ctx context.Context, sessionKey string) error {
	err := c.underlying.Compact(ctx, sessionKey)
	if err != nil {
		return err
	}
	c.cache.invalidate(sessionKey)
	return nil
}

// Close closes the underlying store and clears the cache
func (c *CachedStore) Close() error {
	c.cache.clear()
	return c.underlying.Close()
}

// ListSessions returns all known session keys from the underlying store.
func (c *CachedStore) ListSessions() []string {
	return c.underlying.ListSessions()
}

// Ensure CachedStore implements Store interface
var _ Store = (*CachedStore)(nil)
