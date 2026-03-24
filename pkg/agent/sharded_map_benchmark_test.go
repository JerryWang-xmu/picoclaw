package agent

import (
	"fmt"
	"sync"
	"testing"
)

// BenchmarkShardedMapVsSyncMap compares the performance of shardedTurnStateMap vs sync.Map
func BenchmarkShardedMapVsSyncMap(b *testing.B) {
	benchmarks := []struct {
		name       string
		numKeys    int
		readRatio  int // percentage of reads (0-100)
		writeRatio int // percentage of writes (0-100)
	}{
		{"100Keys_90Read_10Write", 100, 90, 10},
		{"100Keys_50Read_50Write", 100, 50, 50},
		{"1000Keys_90Read_10Write", 1000, 90, 10},
		{"1000Keys_50Read_50Write", 1000, 50, 50},
		{"10000Keys_90Read_10Write", 10000, 90, 10},
		{"10000Keys_50Read_50Write", 10000, 50, 50},
	}

	for _, bm := range benchmarks {
		// Benchmark sync.Map
		b.Run(fmt.Sprintf("SyncMap/%s", bm.name), func(b *testing.B) {
			benchmarkSyncMap(b, bm.numKeys, bm.readRatio, bm.writeRatio)
		})

		// Benchmark sharded map
		b.Run(fmt.Sprintf("ShardedMap/%s", bm.name), func(b *testing.B) {
			benchmarkShardedMap(b, bm.numKeys, bm.readRatio, bm.writeRatio)
		})
	}
}

func benchmarkSyncMap(b *testing.B, numKeys int, readRatio, writeRatio int) {
	m := &sync.Map{}

	// Pre-populate
	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("key-%d", i)
		m.Store(key, &turnState{sessionKey: key})
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("key-%d", i%numKeys)
			op := i % 100

			switch {
			case op < readRatio:
				// Read operation
				m.Load(key)
			case op < readRatio+writeRatio:
				// Write operation
				m.Store(key, &turnState{sessionKey: key})
			default:
				// Delete operation
				m.Delete(key)
			}
			i++
		}
	})
}

func benchmarkShardedMap(b *testing.B, numKeys int, readRatio, writeRatio int) {
	m := newShardedTurnStateMap()

	// Pre-populate
	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("key-%d", i)
		m.Store(key, &turnState{sessionKey: key})
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("key-%d", i%numKeys)
			op := i % 100

			switch {
			case op < readRatio:
				// Read operation
				m.Load(key)
			case op < readRatio+writeRatio:
				// Write operation
				m.Store(key, &turnState{sessionKey: key})
			default:
				// Delete operation
				m.Delete(key)
			}
			i++
		}
	})
}

// BenchmarkShardedMapRange benchmarks the Range operation
func BenchmarkShardedMapRange(b *testing.B) {
	sizes := []int{100, 1000, 10000}

	for _, size := range sizes {
		// sync.Map Range
		b.Run(fmt.Sprintf("SyncMap/Range_%d", size), func(b *testing.B) {
			m := &sync.Map{}
			for i := 0; i < size; i++ {
				key := fmt.Sprintf("key-%d", i)
				m.Store(key, &turnState{sessionKey: key})
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				count := 0
				m.Range(func(key, value any) bool {
					count++
					return true
				})
				_ = count
			}
		})

		// shardedTurnStateMap Range
		b.Run(fmt.Sprintf("ShardedMap/Range_%d", size), func(b *testing.B) {
			m := newShardedTurnStateMap()
			for i := 0; i < size; i++ {
				key := fmt.Sprintf("key-%d", i)
				m.Store(key, &turnState{sessionKey: key})
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				count := 0
				m.Range(func(key string, value *turnState) bool {
					count++
					return true
				})
				_ = count
			}
		})
	}
}

// BenchmarkShardedMapSingleKey benchmarks single-key operations (no contention)
func BenchmarkShardedMapSingleKey(b *testing.B) {
	b.Run("SyncMap", func(b *testing.B) {
		m := &sync.Map{}
		m.Store("key", &turnState{sessionKey: "key"})

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			val, _ := m.Load("key")
			_ = val.(*turnState)
		}
	})

	b.Run("ShardedMap", func(b *testing.B) {
		m := newShardedTurnStateMap()
		m.Store("key", &turnState{sessionKey: "key"})

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.Load("key")
		}
	})
}

// BenchmarkShardedMapContention benchmarks high-contention scenarios
func BenchmarkShardedMapContention(b *testing.B) {
	b.Run("SyncMap/HighContention", func(b *testing.B) {
		m := &sync.Map{}
		m.Store("hotkey", &turnState{sessionKey: "hotkey"})

		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				val, _ := m.Load("hotkey")
				_ = val.(*turnState)
			}
		})
	})

	b.Run("ShardedMap/HighContention", func(b *testing.B) {
		m := newShardedTurnStateMap()
		m.Store("hotkey", &turnState{sessionKey: "hotkey"})

		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				m.Load("hotkey")
			}
		})
	})
}
