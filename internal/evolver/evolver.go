package evolver

import (
	"context"
	"fmt"
	"sync"
	"time"

	"dsl-strategy-evolver/internal/ai"
	"dsl-strategy-evolver/internal/backtest"
	"dsl-strategy-evolver/internal/data"
	"dsl-strategy-evolver/internal/dsl"
	"dsl-strategy-evolver/internal/engine"
	"dsl-strategy-evolver/internal/rank"
)

// Evolver manages the evolution loop
type Evolver struct {
	engine        *engine.Engine
	generator     *ai.Generator
	backtester    *backtest.Backtester
	ranker        *rank.Ranker
	marketRepo    *data.MarketDataRepository
	config        *Config
	stopCh        chan struct{}
	mu            sync.RWMutex
	running       bool
	cycleCount    int
	totalGenerated int
	ctx           context.Context
	cancel        context.CancelFunc
}

// Config holds evolver configuration
type Config struct {
	EvolutionInterval       time.Duration
	StrategiesToGenerate    int
	ReplaceBottomPercent    float64
	MaxStrategies           int
	BacktestDays           int
	DefaultSymbol          string
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		EvolutionInterval:    1 * time.Hour,
		StrategiesToGenerate: 20,
		ReplaceBottomPercent: 20,
		MaxStrategies:        100,
		BacktestDays:        30,
		DefaultSymbol:       "BTCUSDT",
	}
}

// NewEvolver creates a new evolver
func NewEvolver(
	engine *engine.Engine,
	marketRepo *data.MarketDataRepository,
	apiKey, baseURL string,
) *Evolver {
	ctx, cancel := context.WithCancel(context.Background())

	return &Evolver{
		engine:     engine,
		generator:  ai.NewGenerator(apiKey, baseURL),
		backtester: backtest.NewBacktester(marketRepo),
		ranker:     rank.NewRanker(),
		marketRepo: marketRepo,
		config:     DefaultConfig(),
		stopCh:     make(chan struct{}),
		running:    false,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// SetConfig sets the evolver configuration
func (e *Evolver) SetConfig(config *Config) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.config = config
}

// GetConfig returns the current configuration
func (e *Evolver) GetConfig() *Config {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.config
}

// Start starts the evolution loop
func (e *Evolver) Start() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return fmt.Errorf("evolver already running")
	}

	e.running = true

	// Start evolution loop
	go e.evolutionLoop()

	return nil
}

// Stop stops the evolution loop
func (e *Evolver) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return fmt.Errorf("evolver not running")
	}

	e.cancel()
	close(e.stopCh)
	e.running = false

	// Create new context for next start
	e.ctx, e.cancel = context.WithCancel(context.Background())
	e.stopCh = make(chan struct{})

	return nil
}

// evolutionLoop runs the evolution loop
func (e *Evolver) evolutionLoop() {
	ticker := time.NewTicker(e.config.EvolutionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			return
		case <-e.stopCh:
			return
		case <-ticker.C:
			e.RunEvolutionCycle()
		}
	}
}

// RunEvolutionCycle runs a single evolution cycle
func (e *Evolver) RunEvolutionCycle() error {
	fmt.Printf("[Evolver] Starting evolution cycle #%d\n", e.cycleCount+1)

	// 1. Get current leaderboard
	topStrategies := e.ranker.GetTopStrategies(10)

	// 2. Generate new strategies
	fmt.Printf("[Evolver] Generating %d new strategies...\n", e.config.StrategiesToGenerate)

	topStrategyDefs := make([]*dsl.Strategy, 0, len(topStrategies))
	for _, ranked := range topStrategies {
		topStrategyDefs = append(topStrategyDefs, ranked.Strategy)
	}

	newStrategies, err := e.generator.GenerateStrategies(
		e.ctx,
		e.config.StrategiesToGenerate,
		e.config.DefaultSymbol,
		topStrategyDefs,
	)
	if err != nil {
		fmt.Printf("[Evolver] Failed to generate strategies: %v\n", err)
		return err
	}

	e.totalGenerated += len(newStrategies)
	fmt.Printf("[Evolver] Generated %d strategies\n", len(newStrategies))

	// 3. Backtest all new strategies
	var passed []*dsl.Strategy
	for _, strategy := range newStrategies {
		result, err := e.backtester.Run(strategy, e.config.BacktestDays)
		if err != nil {
			fmt.Printf("[Evolver] Backtest failed for %s: %v\n", strategy.Name, err)
			continue
		}

		if result.Passed {
			passed = append(passed, strategy)
			fmt.Printf("[Evolver] %s passed backtest (Sharpe: %.2f, Return: %.2f%%)\n",
				strategy.Name, result.SharpeRatio, result.TotalReturn*100)
		} else {
			fmt.Printf("[Evolver] %s failed backtest criteria\n", strategy.Name)
		}
	}

	fmt.Printf("[Evolver] %d/%d strategies passed backtest\n", len(passed), len(newStrategies))

	// 4. Add to paper trading
	for _, strategy := range passed {
		parser := dsl.NewParser()
		instance, err := parser.ParseWithExpressions([]byte(e.strategyToYAML(strategy)))
		if err != nil {
			fmt.Printf("[Evolver] Failed to compile %s: %v\n", strategy.Name, err)
			continue
		}

		if err := e.engine.AddStrategy(instance); err != nil {
			fmt.Printf("[Evolver] Failed to add %s: %v\n", strategy.Name, err)
			continue
		}

		e.ranker.RegisterStrategy(instance)
	}

	// 5. Replace bottom performers
	currentCount := e.engine.GetActiveStrategyCount()
	removedCount := 0
	if currentCount > e.config.MaxStrategies {
		toRemove := currentCount - e.config.MaxStrategies
		bottom := e.ranker.GetBottomStrategies(toRemove)

		for _, ranked := range bottom {
			e.engine.RemoveStrategy(ranked.StrategyID)
			e.ranker.UnregisterStrategy(ranked.StrategyID)
			fmt.Printf("[Evolver] Removed strategy: %s\n", ranked.Name)
			removedCount++
		}
	}

	// Update metrics for all strategies
	e.updateAllMetrics()

	// Log evolution cycle
	e.cycleCount++
	e.logEvolutionCycle(len(newStrategies), len(passed), removedCount)

	fmt.Printf("[Evolver] Evolution cycle #%d completed. Active strategies: %d\n",
		e.cycleCount, e.engine.GetActiveStrategyCount())

	return nil
}

// updateAllMetrics updates metrics for all strategies
func (e *Evolver) updateAllMetrics() {
	strategies := e.engine.GetAllStrategies()

	for _, instance := range strategies {
		e.ranker.UpdateMetrics(instance.ID, instance.Metrics)
	}
}

// logEvolutionCycle logs evolution cycle to database
func (e *Evolver) logEvolutionCycle(generated, added, removed int) {
	// This would log to the evolution_history table
	// For now, just print
	avgMetrics := e.ranker.GetAverageMetrics()

	fmt.Printf("[Evolver] Cycle stats - Generated: %d, Added: %d, Removed: %d, Avg Sharpe: %.2f\n",
		generated, added, removed, avgMetrics.SharpeRatio)
}

// GetStatus returns the current status
func (e *Evolver) GetStatus() *Status {
	e.mu.RLock()
	defer e.mu.RUnlock()

	avgMetrics := e.ranker.GetAverageMetrics()
	topStrategies := e.ranker.GetTopStrategies(1)

	var bestStrategy *StrategyInfo
	if len(topStrategies) > 0 {
		bestStrategy = &StrategyInfo{
			ID:          topStrategies[0].StrategyID,
			Name:        topStrategies[0].Name,
			SharpeRatio: topStrategies[0].Metrics.SharpeRatio,
			TotalReturn: topStrategies[0].Metrics.TotalReturn,
		}
	}

	return &Status{
		Running:         e.running,
		ActiveStrategies: e.engine.GetActiveStrategyCount(),
		TotalGenerated:  e.totalGenerated,
		TotalCycles:     e.cycleCount,
		NextEvolution:   time.Now().Add(e.config.EvolutionInterval),
		BestStrategy:    bestStrategy,
		AverageMetrics:  avgMetrics,
	}
}

// IsRunning returns whether the evolver is running
func (e *Evolver) IsRunning() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.running
}

// Ranker returns the ranker
func (e *Evolver) Ranker() *rank.Ranker {
	return e.ranker
}

// GenerateStrategies manually generates new strategies
func (e *Evolver) GenerateStrategies(count int, symbol string) ([]*dsl.StrategyInstance, error) {
	// Get top strategies for prompt
	topStrategies := e.ranker.GetTopStrategies(10)
	topStrategyDefs := make([]*dsl.Strategy, 0, len(topStrategies))
	for _, ranked := range topStrategies {
		topStrategyDefs = append(topStrategyDefs, ranked.Strategy)
	}

	// Generate
	newStrategies, err := e.generator.GenerateStrategies(e.ctx, count, symbol, topStrategyDefs)
	if err != nil {
		return nil, err
	}

	// Backtest
	var passed []*dsl.Strategy
	var instances []*dsl.StrategyInstance

	for _, strategy := range newStrategies {
		result, err := e.backtester.Run(strategy, e.config.BacktestDays)
		if err != nil {
			continue
		}

		if result.Passed {
			passed = append(passed, strategy)
		}
	}

	// Create instances
	for _, strategy := range passed {
		parser := dsl.NewParser()
		instance, err := parser.ParseWithExpressions([]byte(e.strategyToYAML(strategy)))
		if err != nil {
			continue
		}
		instances = append(instances, instance)
	}

	return instances, nil
}

// strategyToYAML converts a strategy to YAML
func (e *Evolver) strategyToYAML(strategy *dsl.Strategy) string {
	yaml := fmt.Sprintf(`name: "%s"
symbol: "%s"
type: "%s"
`, strategy.Name, strategy.Symbol, strategy.Type)

	if strategy.Long != nil {
		yaml += fmt.Sprintf(`long:
  entry: "%s"
  exit: "%s"
  stop_loss: %.4f
`, strategy.Long.Entry, strategy.Long.Exit, strategy.Long.StopLoss)
	}

	if strategy.Short != nil {
		yaml += fmt.Sprintf(`short:
  entry: "%s"
  exit: "%s"
  stop_loss: %.4f
`, strategy.Short.Entry, strategy.Short.Exit, strategy.Short.StopLoss)
	}

	yaml += fmt.Sprintf(`risk:
  position_size: %.2f
  max_positions: %d
`, strategy.Risk.PositionSize, strategy.Risk.MaxPositions)

	return yaml
}

// Status represents the evolver status
type Status struct {
	Running          bool                `json:"running"`
	ActiveStrategies int                 `json:"active_strategies"`
	TotalGenerated   int                 `json:"total_generated"`
	TotalCycles      int                 `json:"total_cycles"`
	NextEvolution    time.Time           `json:"next_evolution"`
	BestStrategy     *StrategyInfo       `json:"best_strategy,omitempty"`
	AverageMetrics   *rank.AverageMetrics `json:"average_metrics,omitempty"`
}

// StrategyInfo represents basic strategy info
type StrategyInfo struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	SharpeRatio float64 `json:"sharpe_ratio"`
	TotalReturn float64 `json:"total_return"`
}
