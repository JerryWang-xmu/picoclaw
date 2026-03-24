package memory

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBufferPool_Get(t *testing.T) {
	pool := NewBufferPool()

	// Get a buffer from the pool
	buf := pool.Get()
	require.NotNil(t, buf)
	require.Equal(t, 0, buf.Len())
	require.GreaterOrEqual(t, buf.Cap(), defaultBufferCapacity)

	// Write some data to the buffer
	buf.WriteString("test data")
	require.Equal(t, 9, buf.Len())

	// Return the buffer to the pool
	pool.Put(buf)

	// Get another buffer - should be the same one, reset
	buf2 := pool.Get()
	require.Equal(t, 0, buf2.Len())
	require.GreaterOrEqual(t, buf2.Cap(), defaultBufferCapacity)
}

func TestBufferPool_Put_Nil(t *testing.T) {
	pool := NewBufferPool()

	// Putting nil should not panic
	pool.Put(nil)
}

func TestBufferPool_ResetOnPut(t *testing.T) {
	pool := NewBufferPool()

	// Get a buffer and write sensitive data
	buf := pool.Get()
	buf.WriteString("sensitive data that should be cleared")

	// Return to pool
	pool.Put(buf)

	// Get buffer again - should be empty
	buf2 := pool.Get()
	require.Equal(t, 0, buf2.Len())
}

func TestBufferPool_MultipleGetPut(t *testing.T) {
	pool := NewBufferPool()

	// Get multiple buffers
	bufs := make([]*bytes.Buffer, 5)
	for i := 0; i < 5; i++ {
		bufs[i] = pool.Get()
		bufs[i].WriteString("data")
	}

	// Return all buffers
	for _, buf := range bufs {
		pool.Put(buf)
	}

	// Get buffers again - should be reset
	for i := 0; i < 5; i++ {
		buf := pool.Get()
		require.Equal(t, 0, buf.Len())
		pool.Put(buf)
	}
}

func TestBufferPool_ConcurrentAccess(t *testing.T) {
	pool := NewBufferPool()

	// Run concurrent goroutines that use the pool
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				buf := pool.Get()
				buf.WriteByte('x')
				pool.Put(buf)
			}
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestGetBuffer_PutBuffer(t *testing.T) {
	// Test the package-level functions
	buf := GetBuffer()
	require.NotNil(t, buf)
	require.Equal(t, 0, buf.Len())

	buf.WriteString("test")
	require.Equal(t, 4, buf.Len())

	PutBuffer(buf)

	// Get again - should be reset
	buf2 := GetBuffer()
	require.Equal(t, 0, buf2.Len())
	PutBuffer(buf2)
}

func TestBufferPool_LargeBufferReuse(t *testing.T) {
	pool := NewBufferPool()

	// Get a buffer and grow it significantly
	buf := pool.Get()
	require.NotNil(t, buf)

	// Write more than default capacity to force growth
	largeData := make([]byte, defaultBufferCapacity*2)
	for i := range largeData {
		largeData[i] = byte('x')
	}
	n, err := buf.Write(largeData)
	require.NoError(t, err)
	require.Equal(t, len(largeData), n)
	require.GreaterOrEqual(t, buf.Cap(), len(largeData))

	// Return to pool
	pool.Put(buf)

	// Get again - buffer should be reset and functional
	// Note: sync.Pool may discard objects during GC, so capacity preservation
	// is not guaranteed. We verify the buffer works correctly instead.
	buf2 := pool.Get()
	require.NotNil(t, buf2)
	require.Equal(t, 0, buf2.Len())

	// Buffer should be able to hold large data again
	err = buf2.WriteByte('y')
	require.NoError(t, err)
	require.Equal(t, 1, buf2.Len())
}
