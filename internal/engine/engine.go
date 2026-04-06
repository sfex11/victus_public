package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"dsl-strategy-evolver/internal/data"
	"dsl-strategy-evolver/internal/dsl"
)

// Engine manages paper trading for all strategies
type Engine struct {
	strategies      map[string]*dsl.StrategyInstance
	positionManager *PositionManager
	riskManager     *RiskManager
	marketRepo      *data.MarketDataRepository
	indicatorCalc   *dsl.IndicatorCalculator
	mu              sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
	running         bool
}

// NewEngine creates a new paper trading engine
func NewEngine(marketRepo *data.MarketDataRepository) *Engine {
	ctx, cancel := context.WithCancel(context.Background())

	return &Engine{
		strategies:      make(map[string]*dsl.StrategyInstance),
		positionManager: NewPositionManager(),
		riskManager:     NewRiskManager(),
		marketRepo:      marketRepo,
		indicatorCalc:   dsl.NewIndicatorCalculator(),
		ctx:             ctx,
		cancel:          cancel,
		running:         false,
	}
}

// Start starts the paper trading engine
func (e *Engine) Start() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return fmt.Errorf("engine already running")
	}

	e.running = true

	// Start evaluation loop
	go e.evaluationLoop()

	return nil
}

// Stop stops the paper trading engine
func (e *Engine) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return fmt.Errorf("engine not running")
	}

	e.cancel()
	e.running = false

	return nil
}

// evaluationLoop runs the strategy evaluation loop
func (e *Engine) evaluationLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			e.evaluateAllStrategies()
		}
	}
}

// evaluateAllStrategies evaluates all strategies
func (e *Engine) evaluateAllStrategies() {
	e.mu.RLock()
	strategies := make([]*dsl.StrategyInstance, 0, len(e.strategies))
	for _, s := range e.strategies {
		strategies = append(strategies, s)
	}
	e.mu.RUnlock()

	// Evaluate each strategy
	for _, strategy := range strategies {
		if strategy.State != "active" {
			continue
		}

		e.evaluateStrategy(strategy)
	}
}

// evaluateStrategy evaluates a single strategy
func (e *Engine) evaluateStrategy(instance *dsl.StrategyInstance) {
	// Update context with latest market data
	if err := e.updateStrategyContext(instance); err != nil {
		return
	}

	// Calculate indicators
	if err := e.indicatorCalc.CalculateAll(instance.Context); err != nil {
		return
	}

	// Check entry conditions
	e.checkEntryConditions(instance)

	// Check exit conditions
	e.checkExitConditions(instance)

	// Update positions
	e.updatePositions(instance)

	// Update metrics
	e.updateMetrics(instance)
}

// updateStrategyContext updates the strategy's evaluation context
func (e *Engine) updateStrategyContext(instance *dsl.StrategyInstance) error {
	candles, err := e.marketRepo.GetLatestCandles(instance.Strategy.Symbol, 100)
	if err != nil {
		return err
	}

	if len(candles) == 0 {
		return fmt.Errorf("no candles available")
	}

	// Convert to DSL candles
	dslCandles := make([]*dsl.Candle, len(candles))
	for i, c := range candles {
		dslCandles[i] = &dsl.Candle{
			Symbol:    c.Symbol,
			Open:      c.Open,
			High:      c.High,
			Low:       c.Low,
			Close:     c.Close,
			Volume:    c.Volume,
			Timestamp: c.Timestamp,
		}
	}

	instance.Context.Mu.Lock()
	instance.Context.Candles = dslCandles
	instance.Context.Price = candles[len(candles)-1].Close
	instance.Context.Volume = candles[len(candles)-1].Volume
	instance.Context.Timestamp = candles[len(candles)-1].Timestamp
	instance.Context.Mu.Unlock()

	// Get funding rate
	if fundingRate, err := e.marketRepo.GetLatestFundingRate(instance.Strategy.Symbol); err == nil {
		instance.Context.Mu.Lock()
		instance.Context.FundingRate = fundingRate.Rate
		instance.Context.Mu.Unlock()
	}

	return nil
}

// checkEntryConditions checks if entry conditions are met
func (e *Engine) checkEntryConditions(instance *dsl.StrategyInstance) {
	// Check risk limits
	if !e.riskManager.CanOpenPosition(instance) {
		return
	}

	// Check if already have position for this side
	for _, pos := range instance.Positions {
		if pos.Status == "OPEN" {
			return
		}
	}

	// Check long entry
	if instance.Strategy.Long != nil && instance.LongEntry != nil {
		result, err := instance.LongEntry.Evaluate(instance.Context)
		if err == nil && result > 0 {
			e.openPosition(instance, dsl.LongPosition)
		}
	}

	// Check short entry
	if instance.Strategy.Short != nil && instance.ShortEntry != nil {
		result, err := instance.ShortEntry.Evaluate(instance.Context)
		if err == nil && result > 0 {
			e.openPosition(instance, dsl.ShortPosition)
		}
	}
}

// checkExitConditions checks if exit conditions are met
func (e *Engine) checkExitConditions(instance *dsl.StrategyInstance) {
	for _, pos := range instance.Positions {
		if pos.Status != "OPEN" {
			continue
		}

		// Update current price
		pos.CurrentPrice = instance.Context.Price

		// Calculate unrealized PnL
		if pos.Side == dsl.LongPosition {
			pos.UnrealizedPnL = (pos.CurrentPrice - pos.EntryPrice) / pos.EntryPrice * pos.SizeUSDT
		} else {
			pos.UnrealizedPnL = (pos.EntryPrice - pos.CurrentPrice) / pos.EntryPrice * pos.SizeUSDT
		}

		// Check stop loss
		if pos.StopLoss > 0 {
			if pos.Side == dsl.LongPosition && pos.CurrentPrice <= pos.EntryPrice*(1-pos.StopLoss) {
				e.closePosition(pos, "stop_loss")
				continue
			}
			if pos.Side == dsl.ShortPosition && pos.CurrentPrice >= pos.EntryPrice*(1+pos.StopLoss) {
				e.closePosition(pos, "stop_loss")
				continue
			}
		}

		// Check take profit
		if pos.TakeProfit > 0 {
			if pos.Side == dsl.LongPosition && pos.CurrentPrice >= pos.EntryPrice*(1+pos.TakeProfit) {
				e.closePosition(pos, "take_profit")
				continue
			}
			if pos.Side == dsl.ShortPosition && pos.CurrentPrice <= pos.EntryPrice*(1-pos.TakeProfit) {
				e.closePosition(pos, "take_profit")
				continue
			}
		}

		// Check exit condition
		if pos.Side == dsl.LongPosition && instance.LongExit != nil {
			result, err := instance.LongExit.Evaluate(instance.Context)
			if err == nil && result > 0 {
				e.closePosition(pos, "exit_condition")
				continue
			}
		}

		if pos.Side == dsl.ShortPosition && instance.ShortExit != nil {
			result, err := instance.ShortExit.Evaluate(instance.Context)
			if err == nil && result > 0 {
				e.closePosition(pos, "exit_condition")
				continue
			}
		}
	}
}

// openPosition opens a new position
func (e *Engine) openPosition(instance *dsl.StrategyInstance, side dsl.PositionSide) {
	// Check if already have position for this side
	for _, pos := range instance.Positions {
		if pos.Side == side && pos.Status == "OPEN" {
			return
		}
	}

	position := &dsl.Position{
		ID:           generatePositionID(),
		StrategyID:   instance.ID,
		Symbol:       instance.Strategy.Symbol,
		Side:         side,
		SizeUSDT:     instance.Strategy.Risk.PositionSize,
		EntryPrice:   instance.Context.Price,
		CurrentPrice: instance.Context.Price,
		EntryTime:    time.Now().Unix(),
		Status:       "OPEN",
	}

	// Set stop loss
	if side == dsl.LongPosition && instance.Strategy.Long != nil {
		position.StopLoss = instance.Strategy.Long.StopLoss
	} else if side == dsl.ShortPosition && instance.Strategy.Short != nil {
		position.StopLoss = instance.Strategy.Short.StopLoss
	}

	instance.Positions[position.ID] = position
	e.positionManager.AddPosition(position)
}

// closePosition closes a position
func (e *Engine) closePosition(pos *dsl.Position, reason string) {
	pos.Status = "CLOSED"
	pos.ExitPrice = pos.CurrentPrice
	pos.ExitTime = time.Now().Unix()

	// Calculate realized PnL
	if pos.Side == dsl.LongPosition {
		pos.RealizedPnL = (pos.ExitPrice - pos.EntryPrice) / pos.EntryPrice * pos.SizeUSDT
	} else {
		pos.RealizedPnL = (pos.EntryPrice - pos.ExitPrice) / pos.EntryPrice * pos.SizeUSDT
	}

	e.positionManager.ClosePosition(pos)

	// Record trade
	// (Would be stored in trade history)
}

// updatePositions updates position values
func (e *Engine) updatePositions(instance *dsl.StrategyInstance) {
	for _, pos := range instance.Positions {
		if pos.Status == "OPEN" {
			pos.CurrentPrice = instance.Context.Price

			if pos.Side == dsl.LongPosition {
				pos.UnrealizedPnL = (pos.CurrentPrice - pos.EntryPrice) / pos.EntryPrice * pos.SizeUSDT
			} else {
				pos.UnrealizedPnL = (pos.EntryPrice - pos.CurrentPrice) / pos.EntryPrice * pos.SizeUSDT
			}
		}
	}
}

// updateMetrics updates strategy metrics
func (e *Engine) updateMetrics(instance *dsl.StrategyInstance) {
	// Calculate total return
	totalReturn := 0.0
	totalTrades := 0
	winningTrades := 0
	totalPnL := 0.0
	totalProfit := 0.0
	totalLoss := 0.0

	for _, pos := range instance.Positions {
		if pos.Status == "CLOSED" {
			totalReturn += pos.RealizedPnL
			totalTrades++
			totalPnL += pos.RealizedPnL

			if pos.RealizedPnL > 0 {
				winningTrades++
				totalProfit += pos.RealizedPnL
			} else {
				totalLoss += -pos.RealizedPnL
			}
		}
	}

	// Add unrealized PnL
	for _, pos := range instance.Positions {
		if pos.Status == "OPEN" {
			totalReturn += pos.UnrealizedPnL
		}
	}

	instance.Metrics.TotalReturn = totalReturn
	instance.Metrics.TotalTrades = totalTrades
	instance.Metrics.LastUpdated = time.Now()

	if totalTrades > 0 {
		instance.Metrics.WinRate = float64(winningTrades) / float64(totalTrades)
	}

	if totalLoss > 0 {
		instance.Metrics.ProfitFactor = totalProfit / totalLoss
	} else if totalProfit > 0 {
		instance.Metrics.ProfitFactor = 10.0 // Cap at 10
	}
}

// AddStrategy adds a strategy to the engine
func (e *Engine) AddStrategy(instance *dsl.StrategyInstance) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.strategies[instance.ID]; exists {
		return fmt.Errorf("strategy already exists")
	}

	e.strategies[instance.ID] = instance
	return nil
}

// RemoveStrategy removes a strategy from the engine
func (e *Engine) RemoveStrategy(strategyID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	instance, exists := e.strategies[strategyID]
	if !exists {
		return fmt.Errorf("strategy not found")
	}

	// Close all positions
	for _, pos := range instance.Positions {
		if pos.Status == "OPEN" {
			e.closePosition(pos, "strategy_removed")
		}
	}

	delete(e.strategies, strategyID)
	return nil
}

// GetStrategy returns a strategy by ID
func (e *Engine) GetStrategy(strategyID string) (*dsl.StrategyInstance, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	instance, exists := e.strategies[strategyID]
	if !exists {
		return nil, fmt.Errorf("strategy not found")
	}

	return instance, nil
}

// GetAllStrategies returns all strategies
func (e *Engine) GetAllStrategies() []*dsl.StrategyInstance {
	e.mu.RLock()
	defer e.mu.RUnlock()

	strategies := make([]*dsl.StrategyInstance, 0, len(e.strategies))
	for _, s := range e.strategies {
		strategies = append(strategies, s)
	}

	return strategies
}

// Candle represents market candle data for regime detection
type Candle struct {
	Symbol    string
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	Timestamp int64
}

// GetMarketData returns market data for regime detection
func (e *Engine) GetMarketData(symbol string, limit int) ([]*Candle, error) {
	candles, err := e.marketRepo.GetLatestCandles(symbol, limit)
	if err != nil {
		return nil, err
	}

	result := make([]*Candle, len(candles))
	for i, c := range candles {
		result[i] = &Candle{
			Symbol:    c.Symbol,
			Open:      c.Open,
			High:      c.High,
			Low:       c.Low,
			Close:     c.Close,
			Volume:    c.Volume,
			Timestamp: c.Timestamp,
		}
	}

	return result, nil
}

// GetActiveStrategyCount returns the number of active strategies
func (e *Engine) GetActiveStrategyCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()

	count := 0
	for _, s := range e.strategies {
		if s.State == "active" {
			count++
		}
	}

	return count
}

// IsRunning returns whether the engine is running
func (e *Engine) IsRunning() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.running
}

// GetMarketRepo returns the market data repository
func (e *Engine) GetMarketRepo() *data.MarketDataRepository {
	return e.marketRepo
}

func generatePositionID() string {
	return fmt.Sprintf("pos_%d", time.Now().UnixNano())
}
