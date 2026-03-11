package config

import (
	"fmt"
	"slices"
	"sync"
	"time"
)

// RateLimit defines the allowed request volume for a fixed time window.
type RateLimit struct {
	Requests int           `json:"requests"`
	Window   time.Duration `json:"window"`
}

// Snapshot is the full dynamic configuration state exposed by the config API.
type Snapshot struct {
	DefaultTier string                 `json:"default_tier"`
	Tiers       map[string]RateLimit   `json:"tiers"`
	Tenants     map[string]TenantEntry `json:"tenants"`
}

// TenantEntry associates a tenant with a named tier.
type TenantEntry struct {
	TenantID string `json:"tenant_id"`
	Tier     string `json:"tier"`
}

// ResolvedLimit is the concrete limit used for a tenant request.
type ResolvedLimit struct {
	TenantID string
	Tier     string
	Limit    RateLimit
}

// Store keeps the limiter configuration hot-reloadable without restarting the process.
type Store struct {
	mu   sync.RWMutex
	snap Snapshot
}

// NewStore creates a store with an initial snapshot.
func NewStore(initial Snapshot) (*Store, error) {
	if initial.DefaultTier == "" {
		keys := make([]string, 0, len(initial.Tiers))
		for name := range initial.Tiers {
			keys = append(keys, name)
		}
		slices.Sort(keys)
		if len(keys) > 0 {
			initial.DefaultTier = keys[0]
		}
	}

	if err := validateSnapshot(initial); err != nil {
		return nil, err
	}

	return &Store{snap: cloneSnapshot(initial)}, nil
}

// Snapshot returns a copy of the current configuration.
func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return cloneSnapshot(s.snap)
}

// Resolve returns the concrete rate limit for a tenant.
func (s *Store) Resolve(tenantID string) (ResolvedLimit, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.snap.Tenants[tenantID]
	if !ok {
		entry = TenantEntry{TenantID: tenantID, Tier: s.snap.DefaultTier}
	}

	limit, ok := s.snap.Tiers[entry.Tier]
	if !ok {
		return ResolvedLimit{}, fmt.Errorf("unknown tier %q", entry.Tier)
	}

	return ResolvedLimit{
		TenantID: tenantID,
		Tier:     entry.Tier,
		Limit:    limit,
	}, nil
}

// UpsertTier creates or replaces a tier definition.
func (s *Store) UpsertTier(name string, limit RateLimit) error {
	if name == "" {
		return fmt.Errorf("tier name is required")
	}
	if err := validateRateLimit(limit); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.snap.Tiers == nil {
		s.snap.Tiers = make(map[string]RateLimit)
	}
	s.snap.Tiers[name] = limit
	if s.snap.DefaultTier == "" {
		s.snap.DefaultTier = name
	}

	return nil
}

// UpsertTenant assigns or reassigns a tenant to a tier.
func (s *Store) UpsertTenant(tenantID, tier string) error {
	if tenantID == "" {
		return fmt.Errorf("tenant id is required")
	}
	if tier == "" {
		return fmt.Errorf("tier is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.snap.Tiers[tier]; !ok {
		return fmt.Errorf("unknown tier %q", tier)
	}
	if s.snap.Tenants == nil {
		s.snap.Tenants = make(map[string]TenantEntry)
	}
	s.snap.Tenants[tenantID] = TenantEntry{TenantID: tenantID, Tier: tier}

	return nil
}

// SetDefaultTier updates the fallback tier used by unknown tenants.
func (s *Store) SetDefaultTier(tier string) error {
	if tier == "" {
		return fmt.Errorf("default tier is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.snap.Tiers[tier]; !ok {
		return fmt.Errorf("unknown tier %q", tier)
	}
	s.snap.DefaultTier = tier

	return nil
}

func validateSnapshot(snap Snapshot) error {
	if len(snap.Tiers) == 0 {
		return fmt.Errorf("at least one tier is required")
	}
	for name, limit := range snap.Tiers {
		if name == "" {
			return fmt.Errorf("tier name is required")
		}
		if err := validateRateLimit(limit); err != nil {
			return fmt.Errorf("tier %q: %w", name, err)
		}
	}
	if _, ok := snap.Tiers[snap.DefaultTier]; !ok {
		return fmt.Errorf("default tier %q does not exist", snap.DefaultTier)
	}
	for tenantID, entry := range snap.Tenants {
		if tenantID == "" || entry.TenantID == "" {
			return fmt.Errorf("tenant id is required")
		}
		if entry.TenantID != tenantID {
			return fmt.Errorf("tenant map key %q does not match payload %q", tenantID, entry.TenantID)
		}
		if _, ok := snap.Tiers[entry.Tier]; !ok {
			return fmt.Errorf("tenant %q references unknown tier %q", tenantID, entry.Tier)
		}
	}

	return nil
}

func validateRateLimit(limit RateLimit) error {
	if limit.Requests <= 0 {
		return fmt.Errorf("requests must be greater than zero")
	}
	if limit.Window <= 0 {
		return fmt.Errorf("window must be greater than zero")
	}
	return nil
}

func cloneSnapshot(snap Snapshot) Snapshot {
	out := Snapshot{
		DefaultTier: snap.DefaultTier,
		Tiers:       make(map[string]RateLimit, len(snap.Tiers)),
		Tenants:     make(map[string]TenantEntry, len(snap.Tenants)),
	}
	for name, limit := range snap.Tiers {
		out.Tiers[name] = limit
	}
	for tenantID, entry := range snap.Tenants {
		out.Tenants[tenantID] = entry
	}
	return out
}
