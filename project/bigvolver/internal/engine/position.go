package engine

import (
	"fmt"
	"sync"

	"dsl-strategy-evolver/internal/dsl"
)

// PositionManager manages all positions
type PositionManager struct {
	positions map[string]*dsl.Position
	mu        sync.RWMutex
}

// NewPositionManager creates a new position manager
func NewPositionManager() *PositionManager {
	return &PositionManager{
		positions: make(map[string]*dsl.Position),
	}
}

// AddPosition adds a position
func (pm *PositionManager) AddPosition(pos *dsl.Position) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.positions[pos.ID]; exists {
		return fmt.Errorf("position already exists")
	}

	pm.positions[pos.ID] = pos
	return nil
}

// GetPosition retrieves a position
func (pm *PositionManager) GetPosition(positionID string) (*dsl.Position, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	pos, exists := pm.positions[positionID]
	if !exists {
		return nil, fmt.Errorf("position not found")
	}

	return pos, nil
}

// GetPositionsByStrategy retrieves all positions for a strategy
func (pm *PositionManager) GetPositionsByStrategy(strategyID string) []*dsl.Position {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var positions []*dsl.Position
	for _, pos := range pm.positions {
		if pos.StrategyID == strategyID {
			positions = append(positions, pos)
		}
	}

	return positions
}

// GetOpenPositionsByStrategy retrieves open positions for a strategy
func (pm *PositionManager) GetOpenPositionsByStrategy(strategyID string) []*dsl.Position {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var positions []*dsl.Position
	for _, pos := range pm.positions {
		if pos.StrategyID == strategyID && pos.Status == "OPEN" {
			positions = append(positions, pos)
		}
	}

	return positions
}

// ClosePosition closes a position
func (pm *PositionManager) ClosePosition(pos *dsl.Position) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.positions[pos.ID]; !exists {
		return fmt.Errorf("position not found")
	}

	pos.Status = "CLOSED"
	return nil
}

// GetAllOpenPositions returns all open positions
func (pm *PositionManager) GetAllOpenPositions() []*dsl.Position {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var positions []*dsl.Position
	for _, pos := range pm.positions {
		if pos.Status == "OPEN" {
			positions = append(positions, pos)
		}
	}

	return positions
}

// GetPositionCount returns the total number of positions
func (pm *PositionManager) GetPositionCount() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return len(pm.positions)
}

// GetOpenPositionCount returns the number of open positions
func (pm *PositionManager) GetOpenPositionCount() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	count := 0
	for _, pos := range pm.positions {
		if pos.Status == "OPEN" {
			count++
		}
	}

	return count
}

// Clear removes all positions
func (pm *PositionManager) Clear() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.positions = make(map[string]*dsl.Position)
}
