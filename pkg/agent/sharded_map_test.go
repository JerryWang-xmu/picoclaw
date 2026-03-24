package agent

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShardedTurnStateMap_LoadStoreDelete(t *testing.T) {
	m := newShardedTurnStateMap()

	// Test Load on empty map
	val, ok := m.Load("nonexistent")
	require.False(t, ok)
	require.Nil(t, val)

	// Test Store and Load
	ts1 := &turnState{sessionKey: "session1"}
	m.Store("key1", ts1)

	val, ok = m.Load("key1")
	require.True(t, ok)
	require.Equal(t, ts1, val)

	// Test Load non-existent key
	val, ok = m.Load("key2")
	require.False(t, ok)
	require.Nil(t, val)

	// Test Store overwrite
	ts1Updated := &turnState{sessionKey: "session1_updated"}
	m.Store("key1", ts1Updated)

	val, ok = m.Load("key1")
	require.True(t, ok)
	require.Equal(t, ts1Updated, val)

	// Test Delete
	m.Delete("key1")
	val, ok = m.Load("key1")
	require.False(t, ok)
	require.Nil(t, val)

	// Test Delete non-existent key (should not panic)
	m.Delete("nonexistent")
}

func TestShardedTurnStateMap_EmptyKey(t *testing.T) {
	m := newShardedTurnStateMap()

	// Empty key should be handled gracefully
	m.Store("", &turnState{sessionKey: "empty"})
	val, ok := m.Load("")
	require.False(t, ok)
	require.Nil(t, val)

	m.Delete("") // Should not panic
}

func TestShardedTurnStateMap_NilValue(t *testing.T) {
	m := newShardedTurnStateMap()

	// Store a value first
	ts1 := &turnState{sessionKey: "session1"}
	m.Store("key1", ts1)

	val, ok := m.Load("key1")
	require.True(t, ok)
	require.Equal(t, ts1, val)

	// Store nil value should delete the key
	m.Store("key1", nil)

	val, ok = m.Load("key1")
	require.False(t, ok)
	require.Nil(t, val)
}

func TestShardedTurnStateMap_Range(t *testing.T) {
	m := newShardedTurnStateMap()

	// Test Range on empty map
	count := 0
	m.Range(func(key string, value *turnState) bool {
		count++
		return true
	})
	require.Equal(t, 0, count)

	// Add some values
	ts1 := &turnState{sessionKey: "session1"}
	ts2 := &turnState{sessionKey: "session2"}
	ts3 := &turnState{sessionKey: "session3"}

	m.Store("key1", ts1)
	m.Store("key2", ts2)
	m.Store("key3", ts3)

	// Test Range collects all values
	collected := make(map[string]*turnState)
	m.Range(func(key string, value *turnState) bool {
		collected[key] = value
		return true
	})

	require.Len(t, collected, 3)
	require.Equal(t, ts1, collected["key1"])
	require.Equal(t, ts2, collected["key2"])
	require.Equal(t, ts3, collected["key3"])

	// Test Range early termination
	collected = make(map[string]*turnState)
	m.Range(func(key string, value *turnState) bool {
		collected[key] = value
		return len(collected) < 2 // Stop after collecting 2
	})

	require.Len(t, collected, 2)
}

func TestShardedTurnStateMap_ConcurrentAccess(t *testing.T) {
	m := newShardedTurnStateMap()
	const numGoroutines = 100
	const numOperations = 1000

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 3) // readers, writers, and deleters

	// Concurrent writers
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := string(rune('a' + (id+j)%26))
				m.Store(key, &turnState{sessionKey: key})
			}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := string(rune('a' + (id+j)%26))
				m.Load(key)
			}
		}(i)
	}

	// Concurrent deleters
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := string(rune('a' + (id+j)%26))
				m.Delete(key)
			}
		}(i)
	}

	// Concurrent Range operations
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations/10; j++ {
				m.Range(func(key string, value *turnState) bool {
					return true
				})
			}
		}()
	}

	wg.Wait()
}

func TestShardedTurnStateMap_MultipleShards(t *testing.T) {
	m := newShardedTurnStateMap()

	// Store values that will likely hash to different shards
	for i := 0; i < numShards*2; i++ {
		key := string(rune('a' + i%26)) + string(rune('A' + i/26))
		m.Store(key, &turnState{sessionKey: key})
	}

	// Verify all values are accessible
	count := 0
	m.Range(func(key string, value *turnState) bool {
		count++
		require.Equal(t, key, value.sessionKey)
		return true
	})

	require.Equal(t, numShards*2, count)
}

func TestShardedTurnStateMap_OverwriteSameKey(t *testing.T) {
	m := newShardedTurnStateMap()

	key := "testkey"
	ts1 := &turnState{sessionKey: "v1"}
	ts2 := &turnState{sessionKey: "v2"}
	ts3 := &turnState{sessionKey: "v3"}

	m.Store(key, ts1)
	require.Equal(t, ts1, mustLoad(t, m, key))

	m.Store(key, ts2)
	require.Equal(t, ts2, mustLoad(t, m, key))

	m.Store(key, ts3)
	require.Equal(t, ts3, mustLoad(t, m, key))
}

func mustLoad(t *testing.T, m *shardedTurnStateMap, key string) *turnState {
	t.Helper()
	val, ok := m.Load(key)
	require.True(t, ok)
	return val
}
