package agent

import (
	"hash/fnv"
	"sync"
)

const numShards = 32

// shardedTurnStateMap is a thread-safe sharded map for storing *turnState values.
// It uses 32 shards, each with its own RWMutex, to reduce contention compared to sync.Map.
type shardedTurnStateMap struct {
	shards [numShards]turnStateShard
}

type turnStateShard struct {
	mu   sync.RWMutex
	data map[string]*turnState
}

// newShardedTurnStateMap creates a new sharded map for turn states.
func newShardedTurnStateMap() *shardedTurnStateMap {
	m := &shardedTurnStateMap{}
	for i := range m.shards {
		m.shards[i].data = make(map[string]*turnState)
	}
	return m
}

// getShard returns the shard index for a given key using FNV-1a hash.
func (m *shardedTurnStateMap) getShard(key string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return int(h.Sum32()) % numShards
}

// Load retrieves a value by key. Returns the value and true if found, nil and false otherwise.
func (m *shardedTurnStateMap) Load(key string) (*turnState, bool) {
	if key == "" {
		return nil, false
	}
	shardIdx := m.getShard(key)
	shard := &m.shards[shardIdx]

	shard.mu.RLock()
	defer shard.mu.RUnlock()

	val, ok := shard.data[key]
	return val, ok
}

// Store stores a value for a given key. If value is nil, the key is deleted.
func (m *shardedTurnStateMap) Store(key string, value *turnState) {
	if key == "" {
		return
	}
	shardIdx := m.getShard(key)
	shard := &m.shards[shardIdx]

	shard.mu.Lock()
	defer shard.mu.Unlock()

	if value == nil {
		delete(shard.data, key)
	} else {
		shard.data[key] = value
	}
}

// Delete removes a key from the map.
func (m *shardedTurnStateMap) Delete(key string) {
	if key == "" {
		return
	}
	shardIdx := m.getShard(key)
	shard := &m.shards[shardIdx]

	shard.mu.Lock()
	defer shard.mu.Unlock()

	delete(shard.data, key)
}

// Range iterates over all key-value pairs in the map.
// The iteration order is not specified and may vary.
// If f returns false, iteration stops.
func (m *shardedTurnStateMap) Range(f func(key string, value *turnState) bool) {
	for i := range m.shards {
		shard := &m.shards[i]

		shard.mu.RLock()
		// Copy keys and values to avoid holding lock during callback
		pairs := make([]struct {
			key   string
			value *turnState
		}, 0, len(shard.data))
		for k, v := range shard.data {
			pairs = append(pairs, struct {
				key   string
				value *turnState
			}{k, v})
		}
		shard.mu.RUnlock()

		for _, p := range pairs {
			if !f(p.key, p.value) {
				return
			}
		}
	}
}
