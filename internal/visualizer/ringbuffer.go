package visualizer

import "sync"

// RingBuffer is a thread-safe circular byte buffer.
type RingBuffer struct {
	buf  []byte
	size int
	w    int // write position
	len  int // current fill level
	mu   sync.Mutex
}

// NewRingBuffer creates a ring buffer with the given capacity in bytes.
func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		buf:  make([]byte, size),
		size: size,
	}
}

// Write appends data to the ring buffer, overwriting oldest data if full.
func (rb *RingBuffer) Write(p []byte) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	for _, b := range p {
		rb.buf[rb.w] = b
		rb.w = (rb.w + 1) % rb.size
	}
	rb.len += len(p)
	if rb.len > rb.size {
		rb.len = rb.size
	}
}

// Read returns up to n most recent bytes from the buffer.
func (rb *RingBuffer) Read(n int) []byte {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if n > rb.len {
		n = rb.len
	}
	if n == 0 {
		return nil
	}

	out := make([]byte, n)
	start := (rb.w - n + rb.size) % rb.size
	for i := range n {
		out[i] = rb.buf[(start+i)%rb.size]
	}
	return out
}

// Clear resets the buffer.
func (rb *RingBuffer) Clear() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.w = 0
	rb.len = 0
}
