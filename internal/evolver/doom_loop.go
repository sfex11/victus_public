package evolver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// DoomLoopDetector detects repeated actions (doom-loop)
// Inspired by Quant-Autoresearch's doom-loop detection
type DoomLoopDetector struct {
	fingerprints map[string]*FingerprintRecord
	threshold    int
	mu           sync.RWMutex
	maxAge       time.Duration
}

// FingerprintRecord tracks action history
type FingerprintRecord struct {
	Fingerprint string
	ToolName    string
	Params      map[string]interface{}
	Count       int
	FirstSeen   time.Time
	LastSeen    time.Time
	Blocked     bool
}

// Action represents an action to be checked
type Action struct {
	ToolName string                 `json:"tool_name"`
	Params   map[string]interface{} `json:"parameters"`
}

// NewDoomLoopDetector creates a new doom-loop detector
func NewDoomLoopDetector(threshold int) *DoomLoopDetector {
	return &DoomLoopDetector{
		fingerprints: make(map[string]*FingerprintRecord),
		threshold:    threshold,
		maxAge:       24 * time.Hour, // Reset after 24 hours
	}
}

// Fingerprint generates a unique fingerprint for an action
func (d *DoomLoopDetector) Fingerprint(action Action) string {
	// Sort params for consistent fingerprint
	data, err := json.Marshal(action.Params)
	if err != nil {
		data = []byte("{}")
	}

	hash := sha256.Sum256(append([]byte(action.ToolName+":"), data...))
	return hex.EncodeToString(hash[:8]) // Use first 8 bytes for shorter fingerprint
}

// Check checks if an action is a doom-loop (repeated action)
// Returns: isDoomLoop (should block), count
func (d *DoomLoopDetector) Check(action Action) (bool, int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	fp := d.Fingerprint(action)
	now := time.Now()

	record, exists := d.fingerprints[fp]
	if !exists {
		d.fingerprints[fp] = &FingerprintRecord{
			Fingerprint: fp,
			ToolName:    action.ToolName,
			Params:      action.Params,
			Count:       1,
			FirstSeen:   now,
			LastSeen:    now,
			Blocked:     false,
		}
		return false, 1
	}

	// Check if record is too old (reset)
	if now.Sub(record.FirstSeen) > d.maxAge {
		record.Count = 0
		record.FirstSeen = now
		record.Blocked = false
	}

	record.Count++
	record.LastSeen = now

	// Check threshold
	if record.Count >= d.threshold {
		record.Blocked = true
		return true, record.Count
	}

	return false, record.Count
}

// Record records an action without checking (for tracking)
func (d *DoomLoopDetector) Record(action Action) {
	d.Check(action)
}

// IsBlocked checks if an action is already blocked
func (d *DoomLoopDetector) IsBlocked(action Action) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	fp := d.Fingerprint(action)
	if record, exists := d.fingerprints[fp]; exists {
		return record.Blocked
	}
	return false
}

// GetStats returns statistics about detected doom-loops
func (d *DoomLoopDetector) GetStats() DoomLoopStats {
	d.mu.RLock()
	defer d.mu.RUnlock()

	stats := DoomLoopStats{
		TotalActions:   len(d.fingerprints),
		BlockedActions: 0,
		TopRepeated:    make([]FingerprintRecord, 0),
	}

	for _, record := range d.fingerprints {
		if record.Blocked {
			stats.BlockedActions++
		}
	}

	// Find top repeated actions
	for _, record := range d.fingerprints {
		if record.Count >= 2 {
			stats.TopRepeated = append(stats.TopRepeated, *record)
		}
	}

	// Sort by count (descending)
	for i := 0; i < len(stats.TopRepeated); i++ {
		for j := i + 1; j < len(stats.TopRepeated); j++ {
			if stats.TopRepeated[j].Count > stats.TopRepeated[i].Count {
				stats.TopRepeated[i], stats.TopRepeated[j] = stats.TopRepeated[j], stats.TopRepeated[i]
			}
		}
	}

	// Keep only top 10
	if len(stats.TopRepeated) > 10 {
		stats.TopRepeated = stats.TopRepeated[:10]
	}

	return stats
}

// DoomLoopStats contains doom-loop detection statistics
type DoomLoopStats struct {
	TotalActions   int
	BlockedActions int
	TopRepeated    []FingerprintRecord
}

// Reset clears all fingerprints
func (d *DoomLoopDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.fingerprints = make(map[string]*FingerprintRecord)
}

// CleanExpired removes expired fingerprints
func (d *DoomLoopDetector) CleanExpired() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	expired := 0

	for fp, record := range d.fingerprints {
		if now.Sub(record.LastSeen) > d.maxAge {
			delete(d.fingerprints, fp)
			expired++
		}
	}

	return expired
}

// SetThreshold sets the detection threshold
func (d *DoomLoopDetector) SetThreshold(threshold int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.threshold = threshold
}

// String returns a string representation of stats
func (s DoomLoopStats) String() string {
	result := fmt.Sprintf("DoomLoop Stats: Total=%d, Blocked=%d\n", 
		s.TotalActions, s.BlockedActions)
	
	if len(s.TopRepeated) > 0 {
		result += "Top Repeated Actions:\n"
		for i, record := range s.TopRepeated {
			result += fmt.Sprintf("  %d. %s (count=%d, blocked=%v)\n", 
				i+1, record.ToolName, record.Count, record.Blocked)
		}
	}
	
	return result
}

// ============================================================================
// Strategy Doom-Loop Detection (for AI-generated strategies)
// ============================================================================

// StrategyFingerprint generates a fingerprint for a strategy
// Used to detect if AI is generating similar strategies repeatedly
func StrategyFingerprint(name, entry, exit string, stopLoss, positionSize float64) string {
	data := fmt.Sprintf("%s|%s|%s|%.4f|%.2f", name, entry, exit, stopLoss, positionSize)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:8])
}

// StrategyDoomLoopDetector detects repeated strategy generation
type StrategyDoomLoopDetector struct {
	fingerprints map[string]int // fingerprint -> count
	threshold    int
	mu           sync.RWMutex
}

// NewStrategyDoomLoopDetector creates a new strategy doom-loop detector
func NewStrategyDoomLoopDetector(threshold int) *StrategyDoomLoopDetector {
	return &StrategyDoomLoopDetector{
		fingerprints: make(map[string]int),
		threshold:    threshold,
	}
}

// Check checks if a strategy is a doom-loop
func (d *StrategyDoomLoopDetector) Check(name, entry, exit string, stopLoss, positionSize float64) (bool, int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	fp := StrategyFingerprint(name, entry, exit, stopLoss, positionSize)
	d.fingerprints[fp]++

	count := d.fingerprints[fp]
	return count >= d.threshold, count
}

// Reset clears all fingerprints
func (d *StrategyDoomLoopDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.fingerprints = make(map[string]int)
}
