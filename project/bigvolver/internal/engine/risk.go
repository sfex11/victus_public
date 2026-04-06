package engine

import (
	"dsl-strategy-evolver/internal/dsl"
)

// RiskManager manages risk limits for strategies
type RiskManager struct{}

// NewRiskManager creates a new risk manager
func NewRiskManager() *RiskManager {
	return &RiskManager{}
}

// CanOpenPosition checks if a strategy can open a new position
func (rm *RiskManager) CanOpenPosition(instance *dsl.StrategyInstance) bool {
	// Check max positions limit
	openCount := 0
	for _, pos := range instance.Positions {
		if pos.Status == "OPEN" {
			openCount++
		}
	}

	maxPositions := instance.Strategy.Risk.MaxPositions
	if openCount >= maxPositions {
		return false
	}

	// Check max drawdown
	if instance.Strategy.Risk.MaxDrawdown > 0 && instance.Metrics != nil {
		if instance.Metrics.MaxDrawdown >= instance.Strategy.Risk.MaxDrawdown {
			return false
		}
	}

	return true
}

// CheckPositionSize validates position size
func (rm *RiskManager) CheckPositionSize(instance *dsl.StrategyInstance, size float64) bool {
	maxSize := instance.Strategy.Risk.PositionSize
	return size <= maxSize
}

// CalculatePositionSize calculates appropriate position size
func (rm *RiskManager) CalculatePositionSize(instance *dsl.StrategyInstance, price float64) float64 {
	// Use configured position size
	return instance.Strategy.Risk.PositionSize
}

// ValidateStopLoss validates stop loss setting
func (rm *RiskManager) ValidateStopLoss(instance *dsl.StrategyInstance, stopLoss float64) bool {
	// Stop loss should be between 0.1% and 10%
	return stopLoss >= 0.001 && stopLoss <= 0.10
}

// GetMaxOpenPositions returns the maximum number of open positions
func (rm *RiskManager) GetMaxOpenPositions(instance *dsl.StrategyInstance) int {
	return instance.Strategy.Risk.MaxPositions
}

// CheckRiskLimit checks if strategy is within risk limits
func (rm *RiskManager) CheckRiskLimit(instance *dsl.StrategyInstance) bool {
	// Check if strategy state is active
	if instance.State != "active" {
		return false
	}

	// Check position count
	openPositions := 0
	for _, pos := range instance.Positions {
		if pos.Status == "OPEN" {
			openPositions++
		}
	}

	if openPositions > instance.Strategy.Risk.MaxPositions {
		return false
	}

	return true
}

// CalculateRiskScore calculates a risk score for a strategy
func (rm *RiskManager) CalculateRiskScore(instance *dsl.StrategyInstance) float64 {
	if instance.Metrics == nil {
		return 0.5
	}

	// Risk score based on multiple factors
	score := 0.5

	// Adjust based on max drawdown (higher drawdown = higher risk)
	if instance.Metrics.MaxDrawdown > 0 {
		score += instance.Metrics.MaxDrawdown * 2
	}

	// Adjust based on win rate (lower win rate = higher risk)
	if instance.Metrics.WinRate < 0.5 {
		score += (0.5 - instance.Metrics.WinRate)
	}

	// Normalize to 0-1 range
	if score > 1 {
		score = 1
	}
	if score < 0 {
		score = 0
	}

	return score
}
