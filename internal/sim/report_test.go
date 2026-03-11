package sim

import "testing"

func TestRunShowsDistributedVulnerability(t *testing.T) {
	report, err := Run()
	if err != nil {
		t.Fatalf("run report: %v", err)
	}

	if len(report.Scenarios) != 3 {
		t.Fatalf("expected 3 scenarios, got %d", len(report.Scenarios))
	}

	noisy := report.Scenarios[0]
	if noisy.Strategies[0].Allowed <= noisy.Strategies[1].Allowed {
		t.Fatalf("expected independent nodes to over-admit more traffic than shared limiter")
	}

	failOpen := report.Scenarios[1]
	if failOpen.Strategies[0].Allowed != 2 {
		t.Fatalf("expected both requests to be admitted during fail-open scenario")
	}

	dynamic := report.Scenarios[2]
	if dynamic.Strategies[0].Allowed != 4 || dynamic.Strategies[0].Denied != 1 {
		t.Fatalf("unexpected dynamic config counts: %+v", dynamic.Strategies[0])
	}
}
