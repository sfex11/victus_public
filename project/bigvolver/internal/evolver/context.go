package evolver

import (
	"fmt"
	"sync"
	"time"
)

// ContextCompactor manages context/token pressure for long-running evolution
// Inspired by Quant-Autoresearch's Adaptive Context Compaction (ACC)
type ContextCompactor struct {
	observations    []Observation
	maxObservations int
	compactionRatio float64
	mu              sync.RWMutex
	lastCompaction  time.Time
	stats           CompactionStats
}

// Observation represents an observation in the context
type Observation struct {
	ID         string
	Type       string // "hypothesis", "result", "decision", "error"
	Timestamp  time.Time
	Summary    string
	Details    string
	Importance float64 // 0.0 ~ 1.0
	Compressed bool
}

// CompactionStats tracks compaction statistics
type CompactionStats struct {
	TotalObservations     int
	CompactedObservations int
	LastCompactionRatio   float64
	CompactionCount       int
}

// NewContextCompactor creates a new context compactor
func NewContextCompactor(maxObservations int, compactionRatio float64) *ContextCompactor {
	return &ContextCompactor{
		observations:    make([]Observation, 0),
		maxObservations: maxObservations,
		compactionRatio: compactionRatio,
		stats:           CompactionStats{},
	}
}

// AddObservation adds a new observation
func (c *ContextCompactor) AddObservation(obsType, summary, details string, importance float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	obs := Observation{
		ID:         generateID(),
		Type:       obsType,
		Timestamp:  time.Now(),
		Summary:    summary,
		Details:    details,
		Importance: importance,
		Compressed: false,
	}

	c.observations = append(c.observations, obs)
	c.stats.TotalObservations++

	// Check if compaction needed
	if len(c.observations) >= c.maxObservations {
		c.compact()
	}
}

// compact performs context compaction
func (c *ContextCompactor) compact() {
	if len(c.observations) == 0 {
		return
	}

	targetSize := int(float64(c.maxObservations) * (1 - c.compactionRatio))
	if targetSize < 10 {
		targetSize = 10
	}

	// Sort by importance (descending) + recency
	scored := make([]struct {
		obs   Observation
		score float64
	}, len(c.observations))

	now := time.Now()
	for i, obs := range c.observations {
		// Score = importance * 0.7 + recency * 0.3
		recency := 1.0 - now.Sub(obs.Timestamp).Hours()/24.0
		if recency < 0 {
			recency = 0
		}
		scored[i] = struct {
			obs   Observation
			score float64
		}{obs, obs.Importance*0.7 + recency*0.3}
	}

	// Sort by score (descending)
	for i := 0; i < len(scored); i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[j].score > scored[i].score {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	// Keep top observations
	newObservations := make([]Observation, 0, targetSize)
	compacted := 0

	for i, s := range scored {
		if i < targetSize {
			// Compress details for kept observations
			if !s.obs.Compressed && len(s.obs.Details) > 500 {
				s.obs.Details = summarize(s.obs.Details, 500)
				s.obs.Compressed = true
				compacted++
			}
			newObservations = append(newObservations, s.obs)
		} else {
			compacted++
		}
	}

	c.observations = newObservations
	c.lastCompaction = time.Now()
	c.stats.CompactionCount++
	c.stats.CompactedObservations += compacted
	c.stats.LastCompactionRatio = float64(compacted) / float64(c.stats.TotalObservations)
}

// GetContextUsage returns current context usage percentage
func (c *ContextCompactor) GetContextUsage() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return float64(len(c.observations)) / float64(c.maxObservations) * 100
}

// GetObservations returns all observations
func (c *ContextCompactor) GetObservations() []Observation {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]Observation{}, c.observations...)
}

// GetRecentObservations returns the most recent N observations
func (c *ContextCompactor) GetRecentObservations(n int) []Observation {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if n >= len(c.observations) {
		return append([]Observation{}, c.observations...)
	}

	start := len(c.observations) - n
	return append([]Observation{}, c.observations[start:]...)
}

// GetStats returns compaction statistics
func (c *ContextCompactor) GetStats() CompactionStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stats
}

// ShouldCompact checks if compaction is needed
func (c *ContextCompactor) ShouldCompact(thresholdPercent float64) bool {
	return c.GetContextUsage() >= thresholdPercent
}

// ForceCompact forces a compaction
func (c *ContextCompactor) ForceCompact() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.compact()
}

// Clear clears all observations
func (c *ContextCompactor) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.observations = make([]Observation, 0)
}

// ============================================================================
// Context Status
// ============================================================================

// ContextStatus represents the current context status
type ContextStatus struct {
	UsagePercent      float64
	TotalObservations int
	CompressedRatio   float64
	LastCompaction    time.Time
	Stage             string // "normal", "warning", "critical"
}

// GetStatus returns the current context status
func (c *ContextCompactor) GetStatus() ContextStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	usage := float64(len(c.observations)) / float64(c.maxObservations) * 100
	compressed := 0
	for _, obs := range c.observations {
		if obs.Compressed {
			compressed++
		}
	}

	var compressedRatio float64
	if len(c.observations) > 0 {
		compressedRatio = float64(compressed) / float64(len(c.observations))
	}

	stage := "normal"
	if usage >= 90 {
		stage = "critical"
	} else if usage >= 75 {
		stage = "warning"
	}

	return ContextStatus{
		UsagePercent:      usage,
		TotalObservations: len(c.observations),
		CompressedRatio:   compressedRatio,
		LastCompaction:    c.lastCompaction,
		Stage:             stage,
	}
}

// ============================================================================
// Helper Functions
// ============================================================================

// summarize creates a summary of text
func summarize(text string, maxLength int) string {
	if len(text) <= maxLength {
		return text
	}

	// Simple truncation with ellipsis
	half := maxLength / 2
	return text[:half] + "...[COMPRESSED]..." + text[len(text)-half:]
}

// generateID generates a unique ID
func generateID() string {
	return fmt.Sprintf("obs_%d", time.Now().UnixNano())
}
