package rank

import (
	"sort"
	"sync"

	"dsl-strategy-evolver/internal/dsl"
)

// Ranker calculates strategy rankings based on performance
type Ranker struct {
	metrics    map[string]*dsl.PerformanceMetrics
	mu         sync.RWMutex
	strategies map[string]*dsl.StrategyInstance
}

// NewRanker creates a new ranker
func NewRanker() *Ranker {
	return &Ranker{
		metrics:    make(map[string]*dsl.PerformanceMetrics),
		strategies: make(map[string]*dsl.StrategyInstance),
	}
}

// UpdateMetrics updates metrics for a strategy
func (r *Ranker) UpdateMetrics(strategyID string, metrics *dsl.PerformanceMetrics) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.metrics[strategyID] = metrics
}

// GetMetrics retrieves metrics for a strategy
func (r *Ranker) GetMetrics(strategyID string) *dsl.PerformanceMetrics {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.metrics[strategyID]
}

// GetAllMetrics retrieves all metrics
func (r *Ranker) GetAllMetrics() map[string]*dsl.PerformanceMetrics {
	r.mu.RLock()
	defer r.mu.RUnlock()

	metrics := make(map[string]*dsl.PerformanceMetrics, len(r.metrics))
	for k, v := range r.metrics {
		metrics[k] = v
	}

	return metrics
}

// CalculateScore calculates a composite score for a strategy
func (r *Ranker) CalculateScore(metrics *dsl.PerformanceMetrics) float64 {
	if metrics == nil {
		return 0
	}

	// Weighted score:
	// - Sharpe Ratio: 40%
	// - Win Rate: 30%
	// - Max Drawdown (inverted): 20%
	// - Profit Factor: 10%

	sharpeScore := normalizeSharpe(metrics.SharpeRatio) * 0.4
	winRateScore := metrics.WinRate * 0.3
	drawdownScore := (1 - metrics.MaxDrawdown) * 0.2
	profitFactorScore := normalizeProfitFactor(metrics.ProfitFactor) * 0.1

	score := sharpeScore + winRateScore + drawdownScore + profitFactorScore

	// Normalize to 0-1 range
	if score > 1 {
		score = 1
	}
	if score < 0 {
		score = 0
	}

	return score
}

// normalizeSharpe normalizes Sharpe ratio to 0-1 range
func normalizeSharpe(sharpe float64) float64 {
	// Sharpe > 3 is excellent, < 0 is poor
	if sharpe > 3 {
		return 1
	}
	if sharpe < 0 {
		return 0
	}
	return sharpe / 3
}

// normalizeProfitFactor normalizes profit factor to 0-1 range
func normalizeProfitFactor(pf float64) float64 {
	// PF > 3 is excellent, < 1 is poor
	if pf > 3 {
		return 1
	}
	if pf < 1 {
		return 0
	}
	return (pf - 1) / 2
}

// GetTopStrategies returns the top N strategies by score
func (r *Ranker) GetTopStrategies(n int) []*RankedStrategy {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var rankings []*RankedStrategy

	for strategyID, metrics := range r.metrics {
		score := r.CalculateScore(metrics)

		ranking := &RankedStrategy{
			StrategyID: strategyID,
			Score:      score,
			Metrics:    metrics,
		}

		// Get strategy info
		if strategy, ok := r.strategies[strategyID]; ok {
			ranking.Strategy = strategy.Strategy
			ranking.Name = strategy.Strategy.Name
		}

		rankings = append(rankings, ranking)
	}

	// Sort by score descending
	sort.Slice(rankings, func(i, j int) bool {
		return rankings[i].Score > rankings[j].Score
	})

	// Return top N
	if n > 0 && len(rankings) > n {
		rankings = rankings[:n]
	}

	return rankings
}

// GetBottomStrategies returns the bottom N strategies by score
func (r *Ranker) GetBottomStrategies(n int) []*RankedStrategy {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var rankings []*RankedStrategy

	for strategyID, metrics := range r.metrics {
		score := r.CalculateScore(metrics)

		ranking := &RankedStrategy{
			StrategyID: strategyID,
			Score:      score,
			Metrics:    metrics,
		}

		// Get strategy info
		if strategy, ok := r.strategies[strategyID]; ok {
			ranking.Strategy = strategy.Strategy
			ranking.Name = strategy.Strategy.Name
		}

		rankings = append(rankings, ranking)
	}

	// Sort by score ascending
	sort.Slice(rankings, func(i, j int) bool {
		return rankings[i].Score < rankings[j].Score
	})

	// Return bottom N
	if n > 0 && len(rankings) > n {
		rankings = rankings[:n]
	}

	return rankings
}

// RegisterStrategy registers a strategy for ranking
func (r *Ranker) RegisterStrategy(instance *dsl.StrategyInstance) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.strategies[instance.ID] = instance

	// Initialize metrics if not exists
	if _, exists := r.metrics[instance.ID]; !exists {
		r.metrics[instance.ID] = instance.Metrics
	}
}

// UnregisterStrategy removes a strategy from ranking
func (r *Ranker) UnregisterStrategy(strategyID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.strategies, strategyID)
	delete(r.metrics, strategyID)
}

// GetLeaderboard returns the leaderboard with ranks
func (r *Ranker) GetLeaderboard(n int) []*LeaderboardEntry {
	topStrategies := r.GetTopStrategies(n)

	leaderboard := make([]*LeaderboardEntry, len(topStrategies))
	for i, ranked := range topStrategies {
		leaderboard[i] = &LeaderboardEntry{
			Rank:       i + 1,
			StrategyID: ranked.StrategyID,
			Name:       ranked.Name,
			Score:      ranked.Score,
			SharpeRatio: ranked.Metrics.SharpeRatio,
			WinRate:    ranked.Metrics.WinRate,
			TotalReturn: ranked.Metrics.TotalReturn,
			MaxDrawdown: ranked.Metrics.MaxDrawdown,
		}
	}

	return leaderboard
}

// GetAverageMetrics calculates average metrics across all strategies
func (r *Ranker) GetAverageMetrics() *AverageMetrics {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.metrics) == 0 {
		return &AverageMetrics{}
	}

	avg := &AverageMetrics{}
	count := float64(len(r.metrics))

	for _, metrics := range r.metrics {
		avg.SharpeRatio += metrics.SharpeRatio
		avg.WinRate += metrics.WinRate
		avg.TotalReturn += metrics.TotalReturn
		avg.MaxDrawdown += metrics.MaxDrawdown
		avg.ProfitFactor += metrics.ProfitFactor
		avg.TotalTrades += metrics.TotalTrades
	}

	avg.SharpeRatio /= count
	avg.WinRate /= count
	avg.TotalReturn /= count
	avg.MaxDrawdown /= count
	avg.ProfitFactor /= count
	avg.TotalTrades = int(float64(avg.TotalTrades) / count)

	return avg
}

// RankedStrategy represents a strategy with its ranking
type RankedStrategy struct {
	StrategyID string
	Name       string
	Score      float64
	Strategy   *dsl.Strategy
	Metrics    *dsl.PerformanceMetrics
}

// LeaderboardEntry represents a leaderboard entry
type LeaderboardEntry struct {
	Rank        int     `json:"rank"`
	StrategyID  string  `json:"strategy_id"`
	Name        string  `json:"name"`
	Score       float64 `json:"score"`
	SharpeRatio float64 `json:"sharpe_ratio"`
	WinRate     float64 `json:"win_rate"`
	TotalReturn float64 `json:"total_return"`
	MaxDrawdown float64 `json:"max_drawdown"`
}

// AverageMetrics represents average metrics across strategies
type AverageMetrics struct {
	SharpeRatio float64 `json:"sharpe_ratio"`
	WinRate     float64 `json:"win_rate"`
	TotalReturn float64 `json:"total_return"`
	MaxDrawdown float64 `json:"max_drawdown"`
	ProfitFactor float64 `json:"profit_factor"`
	TotalTrades int     `json:"total_trades"`
}
