package backtest

import (
	"fmt"
	"math"
	"time"

	"dsl-strategy-evolver/internal/data"
	"dsl-strategy-evolver/internal/dsl"
)

// Backtester runs backtests on strategies
type Backtester struct {
	marketRepo    *data.MarketDataRepository
	indicatorCalc *dsl.IndicatorCalculator
	minTrades     int
	minSharpe     float64
	maxDrawdown   float64
}

// NewBacktester creates a new backtester
func NewBacktester(marketRepo *data.MarketDataRepository) *Backtester {
	return &Backtester{
		marketRepo:    marketRepo,
		indicatorCalc: dsl.NewIndicatorCalculator(),
		minTrades:     10,      // Minimum 10 trades
		minSharpe:     0.5,     // Minimum Sharpe ratio
		maxDrawdown:   0.3,     // Maximum 30% drawdown
	}
}

// BacktestResult represents backtest results
type BacktestResult struct {
	StrategyID    string
	TotalReturn   float64
	SharpeRatio   float64
	MaxDrawdown   float64
	WinRate       float64
	ProfitFactor  float64
	TotalTrades   int
	ProfitableTrades int
	AvgWin        float64
	AvgLoss       float64
	LargestWin    float64
	LargestLoss   float64
	AvgHoldingPeriod time.Duration
	StartDate      time.Time
	EndDate        time.Time
	Passed         bool
}

// Run runs a backtest for a strategy
func (b *Backtester) Run(strategy *dsl.Strategy, days int) (*BacktestResult, error) {
	// Get historical data
	candles, err := b.marketRepo.GetHistoricalDataForBacktest(strategy.Symbol, days)
	if err != nil {
		return nil, fmt.Errorf("failed to get historical data: %w", err)
	}

	if len(candles) < 100 {
		return nil, fmt.Errorf("insufficient data for backtest")
	}

	// Compile expressions
	parser := dsl.NewParser()
	longEntry, err := parser.ExprEngine.Compile(strategy.Long.Entry)
	if err != nil {
		return nil, fmt.Errorf("failed to compile long entry: %w", err)
	}
	longExit, err := parser.ExprEngine.Compile(strategy.Long.Exit)
	if err != nil {
		return nil, fmt.Errorf("failed to compile long exit: %w", err)
	}
	shortEntry, err := parser.ExprEngine.Compile(strategy.Short.Entry)
	if err != nil {
		return nil, fmt.Errorf("failed to compile short entry: %w", err)
	}
	shortExit, err := parser.ExprEngine.Compile(strategy.Short.Exit)
	if err != nil {
		return nil, fmt.Errorf("failed to compile short exit: %w", err)
	}

	// Run simulation
	simulation := &Simulation{
		Strategy:     strategy,
		LongEntry:    longEntry,
		LongExit:     longExit,
		ShortEntry:   shortEntry,
		ShortExit:    shortExit,
		IndicatorCalc: b.indicatorCalc,
		StartingCapital: 10000.0, // $10,000 starting capital
	}

	result := simulation.Run(candles)

	// Check if passed filters
	result.Passed = b.IsAcceptable(result)

	return result, nil
}

// IsAcceptable checks if a backtest result meets minimum criteria
func (b *Backtester) IsAcceptable(result *BacktestResult) bool {
	if result.TotalTrades < b.minTrades {
		return false
	}
	if result.SharpeRatio < b.minSharpe {
		return false
	}
	if result.MaxDrawdown > b.maxDrawdown {
		return false
	}
	return true
}

// Simulation represents a backtest simulation
type Simulation struct {
	Strategy       *dsl.Strategy
	LongEntry      *dsl.Expression
	LongExit       *dsl.Expression
	ShortEntry     *dsl.Expression
	ShortExit      *dsl.Expression
	IndicatorCalc  *dsl.IndicatorCalculator
	StartingCapital float64
	CurrentCapital float64
	Positions      []*SimulatedPosition
	Trades         []*Trade
	Candles        []*data.Candle
	Context        *dsl.EvaluationContext
}

// SimulatedPosition represents a position in simulation
type SimulatedPosition struct {
	ID         string
	Side       dsl.PositionSide
	EntryPrice float64
	ExitPrice  float64
	SizeUSDT   float64
	EntryTime  time.Time
	ExitTime   time.Time
	PnL        float64
}

// Trade represents a completed trade
type Trade struct {
	Side       dsl.PositionSide
	EntryPrice float64
	ExitPrice  float64
	PnL        float64
	Duration   time.Duration
}

// Run runs the simulation
func (s *Simulation) Run(candles []*data.Candle) *BacktestResult {
	s.Candles = candles
	s.CurrentCapital = s.StartingCapital
	s.Positions = make([]*SimulatedPosition, 0)
	s.Trades = make([]*Trade, 0)

	s.Context = &dsl.EvaluationContext{
		Indicators: make(map[string]float64),
		Candles:    make([]*dsl.Candle, 0, 100),
	}

	// Run through each candle
	for i := 100; i < len(candles); i++ {
		// Update context with candles up to this point
		history := candles[:i+1]
		s.updateContext(history)

		// Check exit conditions for existing positions
		s.checkExits(i)

		// Check entry conditions
		s.checkEntries(i)

		// Update position values
		s.updatePositionValues(i)
	}

	// Close any remaining positions at the end
	s.closeAllPositions(len(candles) - 1)

	// Calculate results
	return s.calculateResults()
}

// updateContext updates the evaluation context
func (s *Simulation) updateContext(candles []*data.Candle) {
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

	s.Context.Candles = dslCandles
	s.Context.Price = candles[len(candles)-1].Close
	s.Context.Volume = candles[len(candles)-1].Volume
	s.Context.Timestamp = candles[len(candles)-1].Timestamp

	// Calculate indicators
	s.IndicatorCalc.CalculateAll(s.Context)
}

// checkEntries checks entry conditions
func (s *Simulation) checkEntries(candleIndex int) {
	if len(s.Positions) >= s.Strategy.Risk.MaxPositions {
		return
	}

	candle := s.Candles[candleIndex]
	price := candle.Close

	// Check long entry
	if s.Strategy.Long != nil && !s.hasOpenPosition(dsl.LongPosition) {
		result, err := s.LongEntry.Evaluate(s.Context)
		if err == nil && result > 0 {
			s.openPosition(dsl.LongPosition, price, candleIndex)
		}
	}

	// Check short entry
	if s.Strategy.Short != nil && !s.hasOpenPosition(dsl.ShortPosition) {
		result, err := s.ShortEntry.Evaluate(s.Context)
		if err == nil && result > 0 {
			s.openPosition(dsl.ShortPosition, price, candleIndex)
		}
	}
}

// checkExits checks exit conditions for existing positions
func (s *Simulation) checkExits(candleIndex int) {
	candle := s.Candles[candleIndex]
	price := candle.Close

	for i, pos := range s.Positions {
		if pos.ExitPrice > 0 {
			continue // Already closed
		}

		// Check stop loss
		if pos.Side == dsl.LongPosition && price <= pos.EntryPrice*(1-s.Strategy.Long.StopLoss) {
			s.closePosition(i, price, candleIndex, "stop_loss")
			continue
		}
		if pos.Side == dsl.ShortPosition && price >= pos.EntryPrice*(1+s.Strategy.Short.StopLoss) {
			s.closePosition(i, price, candleIndex, "stop_loss")
			continue
		}

		// Check exit condition
		if pos.Side == dsl.LongPosition {
			result, err := s.LongExit.Evaluate(s.Context)
			if err == nil && result > 0 {
				s.closePosition(i, price, candleIndex, "exit")
				continue
			}
		}
		if pos.Side == dsl.ShortPosition {
			result, err := s.ShortExit.Evaluate(s.Context)
			if err == nil && result > 0 {
				s.closePosition(i, price, candleIndex, "exit")
				continue
			}
		}
	}
}

// openPosition opens a new position
func (s *Simulation) openPosition(side dsl.PositionSide, price float64, candleIndex int) {
	candle := s.Candles[candleIndex]

	pos := &SimulatedPosition{
		ID:         fmt.Sprintf("sim_%d", len(s.Positions)),
		Side:       side,
		EntryPrice: price,
		SizeUSDT:   s.Strategy.Risk.PositionSize,
		EntryTime:  time.Unix(candle.Timestamp, 0),
	}

	s.Positions = append(s.Positions, pos)
}

// closePosition closes a position
func (s *Simulation) closePosition(index int, price float64, candleIndex int, reason string) {
	if index >= len(s.Positions) {
		return
	}

	pos := s.Positions[index]
	if pos.ExitPrice > 0 {
		return // Already closed
	}

	candle := s.Candles[candleIndex]
	pos.ExitPrice = price
	pos.ExitTime = time.Unix(candle.Timestamp, 0)

	// Calculate PnL
	if pos.Side == dsl.LongPosition {
		pos.PnL = (pos.ExitPrice - pos.EntryPrice) / pos.EntryPrice * pos.SizeUSDT
	} else {
		pos.PnL = (pos.EntryPrice - pos.ExitPrice) / pos.EntryPrice * pos.SizeUSDT
	}

	s.CurrentCapital += pos.PnL

	// Record trade
	s.Trades = append(s.Trades, &Trade{
		Side:       pos.Side,
		EntryPrice: pos.EntryPrice,
		ExitPrice:  pos.ExitPrice,
		PnL:        pos.PnL,
		Duration:   pos.ExitTime.Sub(pos.EntryTime),
	})
}

// hasOpenPosition checks if there's an open position for a side
func (s *Simulation) hasOpenPosition(side dsl.PositionSide) bool {
	for _, pos := range s.Positions {
		if pos.Side == side && pos.ExitPrice == 0 {
			return true
		}
	}
	return false
}

// updatePositionValues updates unrealized PnL for open positions
func (s *Simulation) updatePositionValues(candleIndex int) {
	// Unrealized PnL calculated in real-time, not stored in simulation
}

// closeAllPositions closes all remaining positions
func (s *Simulation) closeAllPositions(candleIndex int) {
	price := s.Candles[candleIndex].Close

	for i := len(s.Positions) - 1; i >= 0; i-- {
		if s.Positions[i].ExitPrice == 0 {
			s.closePosition(i, price, candleIndex, "end_of_backtest")
		}
	}
}

// calculateResults calculates final backtest results
func (s *Simulation) calculateResults() *BacktestResult {
	result := &BacktestResult{
		StrategyID: s.Strategy.ID,
		StartDate:  time.Unix(s.Candles[0].Timestamp, 0),
		EndDate:    time.Unix(s.Candles[len(s.Candles)-1].Timestamp, 0),
	}

	// Calculate total return
	totalReturn := 0.0
	for _, pos := range s.Positions {
		totalReturn += pos.PnL
	}
	result.TotalReturn = totalReturn / s.StartingCapital

	// Calculate trade statistics
	result.TotalTrades = len(s.Trades)
	profitableTrades := 0
	totalProfit := 0.0
	totalLoss := 0.0
	var wins []float64
	var losses []float64

	for _, trade := range s.Trades {
		if trade.PnL > 0 {
			profitableTrades++
			totalProfit += trade.PnL
			wins = append(wins, trade.PnL)
		} else {
			totalLoss += -trade.PnL
			losses = append(losses, -trade.PnL)
		}
	}

	result.ProfitableTrades = profitableTrades

	if result.TotalTrades > 0 {
		result.WinRate = float64(profitableTrades) / float64(result.TotalTrades)
	}

	if len(wins) > 0 {
		result.AvgWin = totalProfit / float64(len(wins))
		result.LargestWin = maxFloat(wins)
	}

	if len(losses) > 0 {
		result.AvgLoss = totalLoss / float64(len(losses))
		result.LargestLoss = maxFloat(losses)
	}

	if totalLoss > 0 {
		result.ProfitFactor = totalProfit / totalLoss
	} else if totalProfit > 0 {
		result.ProfitFactor = 10.0
	}

	// Calculate Sharpe ratio (simplified)
	result.SharpeRatio = s.calculateSharpeRatio()

	// Calculate max drawdown
	result.MaxDrawdown = s.calculateMaxDrawdown()

	// Calculate average holding period
	if len(s.Trades) > 0 {
		totalDuration := time.Duration(0)
		for _, trade := range s.Trades {
			totalDuration += trade.Duration
		}
		result.AvgHoldingPeriod = totalDuration / time.Duration(len(s.Trades))
	}

	return result
}

// calculateSharpeRatio calculates Sharpe ratio
func (s *Simulation) calculateSharpeRatio() float64 {
	if len(s.Trades) < 2 {
		return 0
	}

	// Calculate returns
	returns := make([]float64, len(s.Trades))
	for i, trade := range s.Trades {
		returns[i] = trade.PnL / s.StartingCapital
	}

	// Calculate mean
	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))

	// Calculate standard deviation
	variance := 0.0
	for _, r := range returns {
		diff := r - mean
		variance += diff * diff
	}
	variance /= float64(len(returns))
	stdDev := math.Sqrt(variance)

	if stdDev == 0 {
		return 0
	}

	// Sharpe ratio (annualized, assuming hourly data)
	// Assuming 24*365 trading periods per year
	// Daily sharpe = mean / stdDev
	// Annualized = Daily * sqrt(365)
	dailySharpe := mean / stdDev
	return dailySharpe * math.Sqrt(365)
}

// calculateMaxDrawdown calculates maximum drawdown
func (s *Simulation) calculateMaxDrawdown() float64 {
	if len(s.Positions) == 0 {
		return 0
	}

	maxDrawdown := 0.0
	peak := s.StartingCapital
	currentEquity := s.StartingCapital

	for _, pos := range s.Positions {
		currentEquity += pos.PnL

		if currentEquity > peak {
			peak = currentEquity
		}

		drawdown := (peak - currentEquity) / peak
		if drawdown > maxDrawdown {
			maxDrawdown = drawdown
		}
	}

	return maxDrawdown
}

func maxFloat(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	max := values[0]
	for _, v := range values[1:] {
		if v > max {
			max = v
		}
	}
	return max
}
