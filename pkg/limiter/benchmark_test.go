package limiter_test

import (
	"context"
	"testing"
	"time"

	"github.com/amin/mesh-tenant-limiter/pkg/config"
	"github.com/amin/mesh-tenant-limiter/pkg/limiter"
)

func BenchmarkLimiterCheck(b *testing.B) {
	store, err := config.NewStore(config.Snapshot{
		DefaultTier: "free",
		Tiers: map[string]config.RateLimit{
			"free": {Requests: 1000000, Window: time.Minute},
		},
	})
	if err != nil {
		b.Fatalf("new store: %v", err)
	}

	lim, err := limiter.New(limiter.Options{Config: store})
	if err != nil {
		b.Fatalf("new limiter: %v", err)
	}
	defer lim.Close()

	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := lim.Check(ctx, "bench-tenant"); err != nil {
			b.Fatalf("check: %v", err)
		}
	}
}

func BenchmarkLimiterCheckParallel(b *testing.B) {
	store, err := config.NewStore(config.Snapshot{
		DefaultTier: "shared",
		Tiers: map[string]config.RateLimit{
			"shared": {Requests: 1000000, Window: time.Minute},
		},
	})
	if err != nil {
		b.Fatalf("new store: %v", err)
	}

	lim, err := limiter.New(limiter.Options{Config: store})
	if err != nil {
		b.Fatalf("new limiter: %v", err)
	}
	defer lim.Close()

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		for pb.Next() {
			if _, err := lim.Check(ctx, "bench-tenant"); err != nil {
				b.Fatalf("check: %v", err)
			}
		}
	})
}
