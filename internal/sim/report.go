package sim

import (
	"context"
	"fmt"
	"time"

	"github.com/amin/mesh-tenant-limiter/pkg/config"
	"github.com/amin/mesh-tenant-limiter/pkg/limiter"
)

// Report is a deterministic, local-only demonstration of the system tradeoffs.
type Report struct {
	GeneratedAt time.Time
	Scenarios   []Scenario
}

// Scenario captures a single demo narrative with multiple strategies.
type Scenario struct {
	Name         string
	Goal         string
	Strategies   []StrategyResult
	Observations []string
}

// StrategyResult summarizes a strategy outcome for one scenario.
type StrategyResult struct {
	Name    string
	Allowed int
	Denied  int
	Notes   []string
}

// Run executes the built-in demo scenarios.
func Run() (Report, error) {
	noisy, err := runNoisyNeighbor()
	if err != nil {
		return Report{}, err
	}

	failOpen, err := runFailOpen()
	if err != nil {
		return Report{}, err
	}

	dynamicConfig, err := runDynamicConfig()
	if err != nil {
		return Report{}, err
	}

	return Report{
		GeneratedAt: time.Now().UTC(),
		Scenarios:   []Scenario{noisy, failOpen, dynamicConfig},
	}, nil
}

func runNoisyNeighbor() (Scenario, error) {
	makeLimiter := func() (*limiter.Limiter, error) {
		store, err := config.NewStore(config.Snapshot{
			DefaultTier: "free",
			Tiers: map[string]config.RateLimit{
				"free": {Requests: 5, Window: time.Minute},
			},
			Tenants: map[string]config.TenantEntry{
				"acme": {TenantID: "acme", Tier: "free"},
			},
		})
		if err != nil {
			return nil, err
		}

		return limiter.New(limiter.Options{
			Config:        store,
			FlushInterval: 25 * time.Millisecond,
		})
	}

	nodeA, err := makeLimiter()
	if err != nil {
		return Scenario{}, err
	}
	defer nodeA.Close()

	nodeB, err := makeLimiter()
	if err != nil {
		return Scenario{}, err
	}
	defer nodeB.Close()

	shared, err := makeLimiter()
	if err != nil {
		return Scenario{}, err
	}
	defer shared.Close()

	localOnly := StrategyResult{Name: "Independent per-node limiters"}
	coordinated := StrategyResult{Name: "Shared global limiter"}

	for i := 0; i < 8; i++ {
		target := nodeA
		if i%2 == 1 {
			target = nodeB
		}

		localDecision, err := target.Check(context.Background(), "acme")
		if err != nil {
			return Scenario{}, err
		}
		if localDecision.Allowed {
			localOnly.Allowed++
		} else {
			localOnly.Denied++
		}

		sharedDecision, err := shared.Check(context.Background(), "acme")
		if err != nil {
			return Scenario{}, err
		}
		if sharedDecision.Allowed {
			coordinated.Allowed++
		} else {
			coordinated.Denied++
		}
	}

	localOnly.Notes = []string{
		"Two nodes each enforce 5 req/min locally, so the tenant sneaks through 8 total requests.",
	}
	coordinated.Notes = []string{
		"A single shared decision-maker blocks the excess after the global quota is exhausted.",
	}

	return Scenario{
		Name: "Noisy Neighbor Across Nodes",
		Goal: "Show why naive per-node limiting breaks down in distributed systems.",
		Strategies: []StrategyResult{
			localOnly,
			coordinated,
		},
		Observations: []string{
			"The vulnerability is not algorithmic quality on one node; it is coordination drift between nodes.",
			"This is the gap the sidecar design is trying to close with shared state and backend synchronization.",
		},
	}, nil
}

func runFailOpen() (Scenario, error) {
	store, err := config.NewStore(config.Snapshot{
		DefaultTier: "free",
		Tiers: map[string]config.RateLimit{
			"free": {Requests: 1, Window: time.Minute},
		},
	})
	if err != nil {
		return Scenario{}, err
	}

	lim, err := limiter.New(limiter.Options{
		Config:               store,
		Backend:              failingBackend{},
		FlushInterval:        5 * time.Millisecond,
		CircuitBreakerWindow: 150 * time.Millisecond,
	})
	if err != nil {
		return Scenario{}, err
	}
	defer lim.Close()

	result := StrategyResult{Name: "Fail-open circuit breaker"}
	for i := 0; i < 2; i++ {
		decision, err := lim.Check(context.Background(), "tenant-outage")
		if err != nil {
			return Scenario{}, err
		}
		if decision.Allowed {
			result.Allowed++
			if decision.FailOpen {
				result.Notes = append(result.Notes, fmt.Sprintf("request %d allowed while backend circuit was open", i+1))
			}
		} else {
			result.Denied++
		}
		if i == 0 {
			time.Sleep(15 * time.Millisecond)
		}
	}

	return Scenario{
		Name:       "Backend Outage",
		Goal:       "Show the system prefers temporary over-admission over blocking good traffic during Redis failures.",
		Strategies: []StrategyResult{result},
		Observations: []string{
			"The first request is admitted normally and triggers a backend sync attempt.",
			"After the sync failure, the limiter opens the circuit and allows the next request in fail-open mode.",
		},
	}, nil
}

func runDynamicConfig() (Scenario, error) {
	store, err := config.NewStore(config.Snapshot{
		DefaultTier: "free",
		Tiers: map[string]config.RateLimit{
			"free": {Requests: 2, Window: time.Minute},
		},
		Tenants: map[string]config.TenantEntry{
			"tenant-growth": {TenantID: "tenant-growth", Tier: "free"},
		},
	})
	if err != nil {
		return Scenario{}, err
	}

	lim, err := limiter.New(limiter.Options{
		Config:        store,
		FlushInterval: 25 * time.Millisecond,
	})
	if err != nil {
		return Scenario{}, err
	}
	defer lim.Close()

	result := StrategyResult{Name: "Hot tier update without restart"}

	for i := 0; i < 3; i++ {
		decision, err := lim.Check(context.Background(), "tenant-growth")
		if err != nil {
			return Scenario{}, err
		}
		if decision.Allowed {
			result.Allowed++
		} else {
			result.Denied++
		}
	}

	if err := store.UpsertTier("free", config.RateLimit{Requests: 5, Window: time.Minute}); err != nil {
		return Scenario{}, err
	}

	for i := 0; i < 2; i++ {
		decision, err := lim.Check(context.Background(), "tenant-growth")
		if err != nil {
			return Scenario{}, err
		}
		if decision.Allowed {
			result.Allowed++
		} else {
			result.Denied++
		}
	}

	result.Notes = []string{
		"Before the config change, the tenant is capped at 2 requests per minute.",
		"After the hot tier update, the same process admits additional traffic with no restart.",
	}

	return Scenario{
		Name:       "Dynamic Configuration",
		Goal:       "Show operators can raise limits live without recycling the sidecar.",
		Strategies: []StrategyResult{result},
		Observations: []string{
			"The store is shared across middleware and the config API, so changes take effect immediately.",
		},
	}, nil
}

type failingBackend struct{}

func (failingBackend) Sync(context.Context, string, config.RateLimit, int) error {
	return context.DeadlineExceeded
}
