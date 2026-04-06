package evolver

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"dsl-strategy-evolver/internal/ai"
	"dsl-strategy-evolver/internal/dsl"
	"gopkg.in/yaml.v3"
)

// AutoReverter handles automatic strategy reversion when performance drops
type AutoReverter struct {
	baselines    map[string]*StrategyBackup
	mu           sync.RWMutex
	revertCount  int
	improveCount int
}

// StrategyBackup stores a backup of a strategy
type StrategyBackup struct {
	ID           string
	YAML         string
	Score        float64
	NunchiScore  float64
	BackedUpAt   time.Time
	RestoredAt   time.Time
	RestoreCount int
}

// NewAutoReverter creates a new auto reverter
func NewAutoReverter() *AutoReverter {
	return &AutoReverter{
		baselines: make(map[string]*StrategyBackup),
	}
}

// Backup creates a backup of the current strategy
func (r *AutoReverter) Backup(id, yaml string, score, nunchiScore float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Only backup if this is a new best
	if existing, ok := r.baselines[id]; ok {
		if score <= existing.Score {
			return // Don't backup worse strategies
		}
	}

	r.baselines[id] = &StrategyBackup{
		ID:          id,
		YAML:        yaml,
		Score:       score,
		NunchiScore: nunchiScore,
		BackedUpAt:  time.Now(),
	}
}

// ShouldRevert checks if we should revert to the backup
func (r *AutoReverter) ShouldRevert(id string, newScore float64) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	backup, ok := r.baselines[id]
	if !ok {
		return false // No backup to revert to
	}

	return newScore < backup.Score
}

// Revert reverts to the backup strategy
func (r *AutoReverter) Revert(id string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	backup, ok := r.baselines[id]
	if !ok {
		return "", false
	}

	backup.RestoreCount++
	backup.RestoredAt = time.Now()
	r.revertCount++

	log.Printf("[REVERT] Restored strategy %s (score=%.4f, restores=%d)", 
		id, backup.Score, backup.RestoreCount)

	return backup.YAML, true
}

// GetStats returns auto-revert statistics
func (r *AutoReverter) GetStats() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return map[string]interface{}{
		"backup_count":   len(r.baselines),
		"revert_count":   r.revertCount,
		"improve_count":  r.improveCount,
	}
}

// ============================================================================
// Enhanced Evolver with Auto-Revert
// ============================================================================

// EnhancedEvolver wraps the standard evolver with auto-revert capability
type EnhancedEvolver struct {
	generator   *ai.Generator
	reverter    *AutoReverter
	doomLoop    *DoomLoopDetector
	context     *ContextCompactor
	playbook    *Playbook
	constitution *Constitution
	mu          sync.RWMutex
}

// NewEnhancedEvolver creates a new enhanced evolver
func NewEnhancedEvolver(generator *ai.Generator, constitutionPath string) (*EnhancedEvolver, error) {
	// Load constitution
	var constitution *Constitution
	if constitutionPath != "" {
		var err error
		constitution, err = Load(constitutionPath)
		if err != nil {
			log.Printf("[EVOLVER] Failed to load constitution: %v, using defaults", err)
			constitution = Default()
		}
	} else {
		constitution = Default()
	}

	return &EnhancedEvolver{
		generator:    generator,
		reverter:     NewAutoReverter(),
		doomLoop:     NewDoomLoopDetector(constitution.Safety.DoomLoopThreshold),
		context:      NewContextCompactor(1000, 0.3),
		constitution: constitution,
	}, nil
}

// SetPlaybook sets the playbook for pattern storage
func (e *EnhancedEvolver) SetPlaybook(playbook *Playbook) {
	e.playbook = playbook
}

// EvolveWithSafety performs strategy evolution with all safety mechanisms
func (e *EnhancedEvolver) EvolveWithSafety(ctx context.Context, symbol, currentSituation string, currentYAML string, currentScore float64) ([]*dsl.Strategy, *EvolutionResult, error) {
	result := &EvolutionResult{
		StartedAt: time.Now(),
		Symbol:    symbol,
	}

	// 1. Check context pressure
	contextStatus := e.context.GetStatus()
	if contextStatus.Stage == "critical" {
		e.context.ForceCompact()
		log.Printf("[EVOLVER] Context compacted (usage was %.1f%%)", contextStatus.UsagePercent)
	}
	result.ContextUsage = contextStatus.UsagePercent

	// 2. Backup current strategy before evolution
	strategyID := fmt.Sprintf("strategy_%d", time.Now().Unix())
	e.reverter.Backup(strategyID, currentYAML, currentScore, currentScore)

	// 3. Generate new strategies using thinking/reasoning pattern
	constitutionMap := e.constitutionToMap()

	strategies, err := e.generator.GenerateWithThinking(ctx, symbol, currentSituation, constitutionMap)
	if err != nil {
		result.Error = err.Error()
		result.FinishedAt = time.Now()
		return nil, result, err
	}

	// 4. Validate strategies against constitution
	validStrategies := e.validateStrategies(strategies)
	result.GeneratedCount = len(strategies)
	result.ValidCount = len(validStrategies)

	// 5. Record observation
	e.context.AddObservation("evolution", 
		fmt.Sprintf("Generated %d strategies (%d valid)", len(strategies), len(validStrategies)),
		currentSituation, 0.5)

	result.FinishedAt = time.Now()
	result.Duration = result.FinishedAt.Sub(result.StartedAt)

	return validStrategies, result, nil
}

// HandleResult handles the result of a strategy evaluation
func (e *EnhancedEvolver) HandleResult(strategyID, yaml string, newScore, newNunchiScore float64) (shouldKeep bool, revertedYAML string) {
	// Check if we should revert
	if e.reverter.ShouldRevert(strategyID, newScore) {
		// Revert to backup
		revertedYAML, _ = e.reverter.Revert(strategyID)
		
		// Record in context
		e.context.AddObservation("revert", 
			fmt.Sprintf("Reverted strategy (score dropped from %.4f to %.4f)", 
				e.reverter.baselines[strategyID].Score, newScore),
			"", 0.3)
		
		return false, revertedYAML
	}

	// Keep the new strategy
	e.reverter.Backup(strategyID, yaml, newScore, newNunchiScore)

	// Store in playbook if score is good
	if e.playbook != nil && newNunchiScore > 0.3 {
		pattern := Pattern{
			ID:           strategyID,
			StrategyYAML: yaml,
			NunchiScore:  newNunchiScore,
			SharpeRatio:  newScore,
			CreatedAt:    time.Now(),
			Symbol:       "BTCUSDT", // Would be passed in real implementation
		}
		if err := e.playbook.StorePattern(pattern); err != nil {
			log.Printf("[EVOLVER] Failed to store pattern: %v", err)
		}
	}

	return true, ""
}

// validateStrategies validates strategies against constitution
func (e *EnhancedEvolver) validateStrategies(strategies []*dsl.Strategy) []*dsl.Strategy {
	var valid []*dsl.Strategy

	for _, s := range strategies {
		// Check doom-loop
		isDoomLoop, count := e.doomLoop.Check(Action{
			ToolName: "generate_strategy",
			Params: map[string]interface{}{
				"name":        s.Name,
				"entry_long":  s.Long.Entry,
				"exit_long":   s.Long.Exit,
			},
		})

		if isDoomLoop {
			log.Printf("[EVOLVER] Skipping doom-loop strategy %s (count=%d)", s.Name, count)
			continue
		}

		// Validate against constitution
		yamlBytes, _ := yaml.Marshal(s)
		if validStrategy, violations := e.constitution.ValidateStrategy(string(yamlBytes)); !validStrategy {
			log.Printf("[EVOLVER] Strategy %s violated constitution: %v", s.Name, violations)
			continue
		}

		valid = append(valid, s)
	}

	return valid
}

// constitutionToMap converts constitution to map for AI prompt
func (e *EnhancedEvolver) constitutionToMap() map[string]interface{} {
	return map[string]interface{}{
		"mandate": e.constitution.Mandate,
		"risk_limits": map[string]interface{}{
			"max_drawdown":       e.constitution.RiskLimits.MaxDrawdown,
			"max_leverage":       e.constitution.RiskLimits.MaxLeverage,
			"max_position_size":  e.constitution.RiskLimits.MaxPositionSize,
			"min_trades":         e.constitution.RiskLimits.MinTrades,
		},
		"goal": map[string]interface{}{
			"metric":    e.constitution.Goal.Metric,
			"threshold": e.constitution.Goal.Threshold,
		},
	}
}

// GetStats returns evolver statistics
func (e *EnhancedEvolver) GetStats() map[string]interface{} {
	stats := map[string]interface{}{
		"reverter":     e.reverter.GetStats(),
		"doom_loop":    e.doomLoop.GetStats(),
		"context":      e.context.GetStats(),
		"constitution": e.constitution.Mandate,
	}

	if e.playbook != nil {
		if playbookStats, err := e.playbook.GetStats(); err == nil {
			stats["playbook"] = playbookStats
		}
	}

	return stats
}

// EvolutionResult represents the result of an evolution cycle
type EvolutionResult struct {
	StartedAt     time.Time
	FinishedAt    time.Time
	Duration      time.Duration
	Symbol        string
	GeneratedCount int
	ValidCount    int
	ContextUsage  float64
	Error         string
}

// String returns a string representation
func (r *EvolutionResult) String() string {
	status := "success"
	if r.Error != "" {
		status = "error: " + r.Error
	}

	return fmt.Sprintf(
		"EvolutionResult[symbol=%s, generated=%d, valid=%d, duration=%v, context=%.1f%%, status=%s]",
		r.Symbol, r.GeneratedCount, r.ValidCount, r.Duration, r.ContextUsage, status,
	)
}
