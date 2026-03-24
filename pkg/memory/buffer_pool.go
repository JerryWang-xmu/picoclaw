package memory

import (
	"bytes"
	"sync"
)

const (
	// defaultBufferCapacity is the initial capacity for pooled buffers.
	// 4KB is a reasonable size for JSON messages, avoiding reallocations
	// for typical message sizes while not wasting memory for small messages.
	defaultBufferCapacity = 4 * 1024
)

// BufferPool is a pool of reusable byte buffers for JSON serialization.
// It uses sync.Pool internally for thread-safe buffer reuse.
type BufferPool struct {
	pool sync.Pool
}

// NewBufferPool creates a new buffer pool with pre-allocated buffers.
func NewBufferPool() *BufferPool {
	return &BufferPool{
		pool: sync.Pool{
			New: func() any {
				buf := make([]byte, 0, defaultBufferCapacity)
				return bytes.NewBuffer(buf)
			},
		},
	}
}

// Get retrieves a buffer from the pool.
// The buffer is reset and ready for use.
func (p *BufferPool) Get() *bytes.Buffer {
	buf := p.pool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

// Put returns a buffer to the pool.
// The buffer is reset before being returned to avoid data leakage.
func (p *BufferPool) Put(buf *bytes.Buffer) {
	if buf == nil {
		return
	}
	// Reset the buffer to clear contents but keep capacity
	buf.Reset()
	p.pool.Put(buf)
}

// defaultPool is the package-level default buffer pool.
// Use GetBuffer() and PutBuffer() for convenient access.
var defaultPool = NewBufferPool()

// GetBuffer retrieves a buffer from the default pool.
// The buffer is reset and ready for use.
func GetBuffer() *bytes.Buffer {
	return defaultPool.Get()
}

// PutBuffer returns a buffer to the default pool.
// The buffer is reset before being returned to avoid data leakage.
func PutBuffer(buf *bytes.Buffer) {
	defaultPool.Put(buf)
}
