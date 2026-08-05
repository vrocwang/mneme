package embeddings

import (
	"context"
	"sync"
	"time"
)

// BatchedProvider wraps an embedding Provider and collects single-text requests
// into batches. Calls to Embed with single-element slices are buffered for up
// to flushInterval, then sent together. Multi-element slices pass through
// immediately. This reduces API round-trips for remote embedding services.
type BatchedProvider struct {
	inner         Provider
	flushInterval time.Duration
	maxBatch      int
	mu            sync.Mutex
	pending       []pendingItem
	timer         *time.Timer
}

type pendingItem struct {
	text string
	ch   chan embedOut
}

type embedOut struct {
	vec []float32
	err error
}

// NewBatchedProvider wraps an embedding provider with batching. flushInterval
// controls how long to wait for more requests before sending. maxBatch caps
// the batch size; when reached the batch flushes immediately.
func NewBatchedProvider(inner Provider, flushInterval time.Duration, maxBatch int) *BatchedProvider {
	return &BatchedProvider{
		inner:         inner,
		flushInterval: flushInterval,
		maxBatch:      maxBatch,
	}
}

func (b *BatchedProvider) Name() string    { return b.inner.Name() }
func (b *BatchedProvider) Dimensions() int { return b.inner.Dimensions() }

func (b *BatchedProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	// Pass through batches that are already multi-element.
	if len(texts) != 1 {
		return b.inner.Embed(ctx, texts)
	}

	ch := make(chan embedOut, 1)
	b.mu.Lock()
	b.pending = append(b.pending, pendingItem{text: texts[0], ch: ch})
	if len(b.pending) >= b.maxBatch {
		b.flushLocked()
	} else if b.timer == nil {
		b.timer = time.AfterFunc(b.flushInterval, func() {
			b.mu.Lock()
			b.flushLocked()
			b.mu.Unlock()
		})
	}
	b.mu.Unlock()

	out := <-ch
	if out.err != nil {
		return nil, out.err
	}
	return [][]float32{out.vec}, nil
}

func (b *BatchedProvider) flushLocked() {
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	if len(b.pending) == 0 {
		return
	}
	items := b.pending
	b.pending = nil

	texts := make([]string, len(items))
	for i, item := range items {
		texts[i] = item.text
	}
	vecs, err := b.inner.Embed(context.Background(), texts)
	for i, item := range items {
		if err != nil {
			item.ch <- embedOut{err: err}
		} else if i < len(vecs) {
			item.ch <- embedOut{vec: vecs[i]}
		} else {
			item.ch <- embedOut{}
		}
	}
}
