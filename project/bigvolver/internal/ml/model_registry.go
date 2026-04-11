package ml

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ModelVersion tracks a single model version
type ModelVersion struct {
	Version     string    `json:"version"`
	TrainedAt   time.Time `json:"trained_at"`
	SharpeRatio float64   `json:"sharpe_ratio"`
	WinRate     float64   `json:"win_rate"`
	SamplesUsed int       `json:"samples_used"`
	Active      bool      `json:"active"`
}

// ModelRegistry manages model version history and automatic rollback
type ModelRegistry struct {
	versions          []ModelVersion
	current           *ModelVersion
	maxHistory        int
	rollbackThreshold float64 // Sharpe drop % to trigger rollback
	persistencePath   string
	mu                sync.RWMutex
}

// NewModelRegistry creates a new model registry
func NewModelRegistry(opts ...RegistryOption) *ModelRegistry {
	r := &ModelRegistry{
		versions:          make([]ModelVersion, 0),
		maxHistory:        10,
		rollbackThreshold: 30.0, // 30% Sharpe drop
		persistencePath:   "./models/registry.json",
	}

	for _, opt := range opts {
		opt(r)
	}

	// Try to load from disk
	r.load()

	return r
}

// RegistryOption configures the model registry
type RegistryOption func(*ModelRegistry)

// WithMaxHistory sets the maximum number of versions to keep
func WithMaxHistory(n int) RegistryOption {
	return func(r *ModelRegistry) { r.maxHistory = n }
}

// WithRollbackThreshold sets the Sharpe drop % to trigger rollback
func WithRollbackThreshold(pct float64) RegistryOption {
	return func(r *ModelRegistry) { r.rollbackThreshold = pct }
}

// WithPersistencePath sets the file path for registry persistence
func WithPersistencePath(path string) RegistryOption {
	return func(r *ModelRegistry) { r.persistencePath = path }
}

// RecordVersion adds a new model version to the registry
func (r *ModelRegistry) RecordVersion(v ModelVersion) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Deactivate all previous versions
	for i := range r.versions {
		r.versions[i].Active = false
	}

	if v.Active {
		r.current = &v
	}

	r.versions = append(r.versions, v)

	// Trim to maxHistory
	if len(r.versions) > r.maxHistory {
		r.versions = r.versions[len(r.versions)-r.maxHistory:]
	}

	return r.save()
}

// ShouldRollback checks if the new version performs worse than the current
// Returns (shouldRollback, reason)
func (r *ModelRegistry) ShouldRollback(newVersion ModelVersion) (bool, string) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.current == nil {
		// No previous version — accept the new one
		return false, ""
	}

	prev := r.current

	// Condition 1: Sharpe < 0
	if newVersion.SharpeRatio < 0 {
		return true, fmt.Sprintf(
			"new model Sharpe is negative (%.4f < 0)", newVersion.SharpeRatio)
	}

	// Condition 2: Sharpe dropped by more than threshold
	if prev.SharpeRatio > 0 {
		dropPct := (prev.SharpeRatio - newVersion.SharpeRatio) / prev.SharpeRatio * 100
		if dropPct > r.rollbackThreshold {
			return true, fmt.Sprintf(
				"Sharpe dropped %.1f%% (%.4f → %.4f), threshold is %.1f%%",
				dropPct, prev.SharpeRatio, newVersion.SharpeRatio, r.rollbackThreshold)
		}
	}

	return false, ""
}

// ActivateVersion activates a specific model version
func (r *ModelRegistry) ActivateVersion(version string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i := range r.versions {
		if r.versions[i].Version == version {
			// Deactivate all
			for j := range r.versions {
				r.versions[j].Active = false
			}
			r.versions[i].Active = true
			r.current = &r.versions[i]
			return r.save()
		}
	}

	return fmt.Errorf("version %s not found", version)
}

// GetCurrent returns the currently active model version
func (r *ModelRegistry) GetCurrent() *ModelVersion {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.current
}

// GetHistory returns all version history
func (r *ModelRegistry) GetHistory() []ModelVersion {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]ModelVersion, len(r.versions))
	copy(result, r.versions)
	return result
}

// save persists the registry to disk
func (r *ModelRegistry) save() error {
	if r.persistencePath == "" {
		return nil
	}

	dir := filepath.Dir(r.persistencePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(r.versions, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal registry: %w", err)
	}

	return os.WriteFile(r.persistencePath, data, 0644)
}

// load restores the registry from disk
func (r *ModelRegistry) load() {
	if r.persistencePath == "" {
		return
	}

	data, err := os.ReadFile(r.persistencePath)
	if err != nil {
		return // No file yet — start fresh
	}

	if err := json.Unmarshal(data, &r.versions); err != nil {
		return
	}

	// Find the active version
	for i := range r.versions {
		if r.versions[i].Active {
			r.current = &r.versions[i]
			break
		}
	}
}
