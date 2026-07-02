package pendingqueue

import (
	"context"
	"errors"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// ErrQueueFull is returned by Memory.Enqueue when maxTotal is reached.
// Callers on the hot path should treat this exactly like "pool unknown,
// no eth_call, no queue" — skip writing a SwapRecord for this event and
// move on; it is not a fatal error.
var ErrQueueFull = errors.New("pendingqueue: queue full")

// Memory is an in-memory Queue. Everything is lost on process restart —
// acceptable here because losing a queued message only means the affected
// swap never gets a SwapRecord row (model.Transaction for it is already
// durably written independently), not silent data corruption. A future
// Kafka-backed implementation of Queue would remove this restart-loses-
// pending-items characteristic for anyone who needs it, without changing
// any caller.
type Memory struct {
	mu       sync.Mutex
	groups   map[string][]Message
	attempts map[string]int
	total    int

	maxTotal     int
	maxAge       time.Duration
	maxAttempts  int
	pollInterval time.Duration
}

// NewMemory creates an in-memory Queue.
//
//	maxTotal:    overall capacity across all pools combined — bounds worst
//	             case memory use if many new pools show up at once.
//	maxAge:      a pool's oldest queued message older than this gets
//	             dropped (with the rest of that pool's batch) rather than
//	             retried forever — resolution is either going to succeed
//	             quickly or the RPC node has a real problem; retrying a
//	             swap from an hour ago indefinitely isn't useful.
//	maxAttempts: same idea, bounded by attempt count instead of wall time.
//	pollInterval: how often Consume scans for pending work. This is a
//	             background worker, not the hot path, so a few hundred ms
//	             of added latency for resolution is a non-issue — polling
//	             keeps the implementation simple (no precise wake signaling
//	             to get subtly wrong) at a cost nobody will notice.
func NewMemory(maxTotal int, maxAge time.Duration, maxAttempts int, pollInterval time.Duration) *Memory {
	return &Memory{
		groups:       make(map[string][]Message),
		attempts:     make(map[string]int),
		maxTotal:     maxTotal,
		maxAge:       maxAge,
		maxAttempts:  maxAttempts,
		pollInterval: pollInterval,
	}
}

// Enqueue implements Queue.
func (m *Memory) Enqueue(ctx context.Context, msg Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.total >= m.maxTotal {
		return ErrQueueFull
	}

	msg.EnqueuedAt = time.Now()
	m.groups[msg.PoolAddr] = append(m.groups[msg.PoolAddr], msg)
	m.total++
	return nil
}

// Consume implements Queue.
func (m *Memory) Consume(ctx context.Context, handler func(ctx context.Context, poolAddr string, msgs []Message) error) error {
	ticker := time.NewTicker(m.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			m.processOnce(ctx, handler)
		}
	}
}

func (m *Memory) processOnce(ctx context.Context, handler func(ctx context.Context, poolAddr string, msgs []Message) error) {
	m.mu.Lock()
	pools := make([]string, 0, len(m.groups))
	for addr, msgs := range m.groups {
		if len(msgs) > 0 {
			pools = append(pools, addr)
		}
	}
	m.mu.Unlock()

	for _, addr := range pools {
		m.mu.Lock()
		msgs := m.groups[addr]
		m.mu.Unlock()
		if len(msgs) == 0 {
			continue
		}

		// Handler runs without the lock held — it may do RPC/DB/Redis
		// work, and holding the lock across that would serialize every
		// pool's resolution behind whichever one is currently in flight.
		err := handler(ctx, addr, msgs)

		m.mu.Lock()
		if err != nil {
			m.attempts[addr]++
			expired := time.Since(msgs[0].EnqueuedAt) > m.maxAge
			exhausted := m.attempts[addr] >= m.maxAttempts
			if expired || exhausted {
				log.Warnf("[pendingqueue] giving up on pool %s after %d attempt(s) (%d message(s) dropped, reason: %s)",
					addr, m.attempts[addr], len(m.groups[addr]), dropReason(expired, exhausted))
				m.total -= len(m.groups[addr])
				delete(m.groups, addr)
				delete(m.attempts, addr)
			}
			// else: leave queued as-is (including anything enqueued
			// concurrently while handler was running) for the next poll.
		} else {
			// Only remove what we actually handed to handler — anything
			// enqueued for this pool while handler was running stays
			// queued for the next round rather than being silently
			// dropped as "already processed."
			remaining := m.groups[addr][len(msgs):]
			if len(remaining) == 0 {
				delete(m.groups, addr)
			} else {
				m.groups[addr] = remaining
			}
			m.total -= len(msgs)
			delete(m.attempts, addr)
		}
		m.mu.Unlock()
	}
}

func dropReason(expired, exhausted bool) string {
	switch {
	case expired && exhausted:
		return "max age and max attempts both exceeded"
	case expired:
		return "max age exceeded"
	default:
		return "max attempts exceeded"
	}
}
