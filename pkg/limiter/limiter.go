package limiter

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amin/mesh-tenant-limiter/pkg/config"
	"github.com/amin/mesh-tenant-limiter/pkg/telemetry"
)

// Backend receives asynchronous flushes of locally admitted requests.
type Backend interface {
	Sync(ctx context.Context, tenantID string, limit config.RateLimit, count int) error
}

// Decision represents the result of a limiter check.
type Decision struct {
	Allowed   bool
	FailOpen  bool
	TenantID  string
	Tier      string
	Limit     int
	Remaining int
	ResetAt   time.Time
}

// Options configure the limiter behavior.
type Options struct {
	Config               *config.Store
	Backend              Backend
	Metrics              *telemetry.Metrics
	QueueSize            int
	FlushInterval        time.Duration
	CircuitBreakerWindow time.Duration
	Clock                func() time.Time
}

// Limiter applies tenant-aware rate limits using a fast local sliding window and async backend sync.
type Limiter struct {
	config     *config.Store
	backend    Backend
	metrics    *telemetry.Metrics
	queue      chan syncEvent
	stop       chan struct{}
	done       chan struct{}
	clock      func() time.Time
	flushEvery time.Duration
	breakerFor time.Duration

	windows sync.Map
	openTil atomic.Int64
}

type syncEvent struct {
	tenantID string
	limit    config.RateLimit
}

type syncItem struct {
	tenantID string
	limit    config.RateLimit
	count    int
}

type tenantWindow struct {
	mu         sync.Mutex
	timestamps []time.Time
}

// New creates and starts a limiter instance.
func New(opts Options) (*Limiter, error) {
	if opts.Config == nil {
		return nil, errors.New("config store is required")
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	if opts.QueueSize <= 0 {
		opts.QueueSize = 4096
	}
	if opts.FlushInterval <= 0 {
		opts.FlushInterval = 250 * time.Millisecond
	}
	if opts.CircuitBreakerWindow <= 0 {
		opts.CircuitBreakerWindow = 5 * time.Second
	}

	l := &Limiter{
		config:     opts.Config,
		backend:    opts.Backend,
		metrics:    opts.Metrics,
		queue:      make(chan syncEvent, opts.QueueSize),
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
		clock:      opts.Clock,
		flushEvery: opts.FlushInterval,
		breakerFor: opts.CircuitBreakerWindow,
	}

	go l.run()
	return l, nil
}

// Close stops the background flush worker.
func (l *Limiter) Close() error {
	close(l.stop)
	<-l.done
	return nil
}

// Check evaluates a tenant request against its sliding window.
func (l *Limiter) Check(ctx context.Context, tenantID string) (Decision, error) {
	resolved, err := l.config.Resolve(tenantID)
	if err != nil {
		return Decision{}, fmt.Errorf("resolve config: %w", err)
	}

	now := l.clock()
	if l.backend != nil && l.circuitOpen() {
		decision := Decision{
			Allowed:   true,
			FailOpen:  true,
			TenantID:  tenantID,
			Tier:      resolved.Tier,
			Limit:     resolved.Limit.Requests,
			Remaining: resolved.Limit.Requests,
			ResetAt:   now.Add(resolved.Limit.Window),
		}
		if l.metrics != nil {
			l.metrics.RecordDecision(tenantID, true, true)
		}
		return decision, nil
	}

	window := l.windowFor(tenantID)
	allowed, remaining, resetAt := window.allow(now, resolved.Limit)
	decision := Decision{
		Allowed:   allowed,
		TenantID:  tenantID,
		Tier:      resolved.Tier,
		Limit:     resolved.Limit.Requests,
		Remaining: remaining,
		ResetAt:   resetAt,
	}

	if allowed {
		l.enqueueSync(ctx, tenantID, resolved.Limit)
	}
	if l.metrics != nil {
		l.metrics.RecordDecision(tenantID, allowed, false)
	}
	return decision, nil
}

// FormatHeaders returns the standard rate-limit response headers for a decision.
func FormatHeaders(decision Decision) map[string]string {
	return map[string]string{
		"X-RateLimit-Limit":     strconv.Itoa(decision.Limit),
		"X-RateLimit-Remaining": strconv.Itoa(decision.Remaining),
		"X-RateLimit-Reset":     strconv.FormatInt(decision.ResetAt.Unix(), 10),
	}
}

func (l *Limiter) windowFor(tenantID string) *tenantWindow {
	win, _ := l.windows.LoadOrStore(tenantID, &tenantWindow{})
	return win.(*tenantWindow)
}

func (l *Limiter) enqueueSync(_ context.Context, tenantID string, limit config.RateLimit) {
	if l.backend == nil || l.circuitOpen() {
		return
	}

	select {
	case l.queue <- syncEvent{tenantID: tenantID, limit: limit}:
	default:
		if l.metrics != nil {
			l.metrics.RecordSyncDropped()
		}
	}
}

func (l *Limiter) circuitOpen() bool {
	until := time.Unix(0, l.openTil.Load())
	open := until.After(l.clock())
	if l.metrics != nil {
		l.metrics.SetCircuitOpen(open)
	}
	return open
}

func (l *Limiter) openCircuit() {
	l.openTil.Store(l.clock().Add(l.breakerFor).UnixNano())
	if l.metrics != nil {
		l.metrics.SetCircuitOpen(true)
	}
}

func (l *Limiter) closeCircuit() {
	l.openTil.Store(0)
	if l.metrics != nil {
		l.metrics.SetCircuitOpen(false)
	}
}

func (l *Limiter) run() {
	defer close(l.done)

	ticker := time.NewTicker(l.flushEvery)
	defer ticker.Stop()

	pending := make(map[string]syncItem)
	flush := func() {
		if len(pending) == 0 || l.backend == nil {
			return
		}

		for tenantID, item := range pending {
			start := l.clock()
			err := l.backend.Sync(context.Background(), item.tenantID, item.limit, item.count)
			if err != nil {
				if l.metrics != nil {
					l.metrics.RecordBackendError()
					l.metrics.ObserveSyncLatency("error", l.clock().Sub(start))
				}
				l.openCircuit()
				return
			}

			if l.metrics != nil {
				l.metrics.ObserveSyncLatency("success", l.clock().Sub(start))
			}
			delete(pending, tenantID)
		}

		l.closeCircuit()
	}

	for {
		select {
		case <-l.stop:
			flush()
			return
		case <-ticker.C:
			if l.circuitOpen() {
				continue
			}
			flush()
		case event := <-l.queue:
			item := pending[event.tenantID]
			item.tenantID = event.tenantID
			item.limit = event.limit
			item.count++
			pending[event.tenantID] = item
		}
	}
}

func (w *tenantWindow) allow(now time.Time, limit config.RateLimit) (bool, int, time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()

	cutoff := now.Add(-limit.Window)
	expired := 0
	for expired < len(w.timestamps) && !w.timestamps[expired].After(cutoff) {
		expired++
	}
	if expired > 0 {
		copy(w.timestamps, w.timestamps[expired:])
		w.timestamps = w.timestamps[:len(w.timestamps)-expired]
	}

	if len(w.timestamps) >= limit.Requests {
		resetAt := w.timestamps[0].Add(limit.Window)
		return false, 0, resetAt
	}

	w.timestamps = append(w.timestamps, now)
	resetAt := now.Add(limit.Window)
	return true, limit.Requests - len(w.timestamps), resetAt
}
