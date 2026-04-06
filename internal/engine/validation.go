package engine

import (
	"math"
	"math/rand"
	"sort"
	"time"
)

// MonteCarloResult holds permutation test results
type MonteCarloResult struct {
	ActualSharpe      float64 `json:"actual_sharpe"`
	MeanPermutedSharpe float64 `json:"mean_permuted_sharpe"`
	StdPermutedSharpe  float64 `json:"std_permuted_sharpe"`
	PValue            float64 `json:"p_value"`
	IsSignificant     bool    `json:"is_significant"`
	NPermutations     int     `json:"n_permutations"`
}

// WalkForwardResult holds walk-forward validation results
type WalkForwardResult struct {
	Windows          []WindowResult   `json:"windows"`
	AvgOOSSharpe     float64          `json:"avg_oos_sharpe"`
	AvgOOSMaxDD      float64          `json:"avg_oos_max_dd"`
	TotalOOSTrades   float64          `json:"total_oos_trades"`
	OOSWinRate       float64          `json:"oos_win_rate"`
	AvgOOSReturn     float64          `json:"avg_oos_return"`
	ConsistencyScore float64          `json:"consistency_score"`
	SharpeStability  float64          `json:"sharpe_stability"`
	MonteCarlo       *MonteCarloResult `json:"monte_carlo,omitempty"`
	CombinedScore    float64          `json:"combined_score"`
	DurationMs       int64            `json:"duration_ms"`
}

// WindowResult holds single window metrics
type WindowResult struct {
	Index      int     `json:"index"`
	TrainStart int64   `json:"train_start"`
	TrainEnd   int64   `json:"train_end"`
	TestStart  int64   `json:"test_start"`
	TestEnd    int64   `json:"test_end"`
	OSSSharpe  float64 `json:"oos_sharpe"`
	OOSMaxDD   float64 `json:"oos_max_dd"`
	OOSTrades  int     `json:"oos_trades"`
	OOSWinRate float64 `json:"oos_win_rate"`
	OOSReturn  float64 `json:"oos_return"`
}

// WalkForwardConfig holds walk-forward parameters
type WalkForwardConfig struct {
	NWindows         int   `json:"n_windows"`
	TrainRatio       float64 `json:"train_ratio"`
	EnableMonteCarlo bool  `json:"enable_monte_carlo"`
	NPermutations    int   `json:"n_permutations"`
	SignalLag        int   `json:"signal_lag"`
}

// DefaultWalkForwardConfig returns sensible defaults
func DefaultWalkForwardConfig() WalkForwardConfig {
	return WalkForwardConfig{
		NWindows:         5,
		TrainRatio:       0.7,
		EnableMonteCarlo: true,
		NPermutations:    1000,
		SignalLag:        1,
	}
}

// ============================================================================
// Sharpe / Sortino / MDD / WinRate / ProfitFactor
// ============================================================================

// CalculateSharpe returns annualized Sharpe ratio (252 trading days)
func CalculateSharpe(returns []float64) float64 {
	if len(returns) == 0 {
		return 0.0
	}

	n := float64(len(returns))
	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= n

	variance := 0.0
	for _, r := range returns {
		variance += (r - mean) * (r - mean)
	}
	variance /= n
	std := math.Sqrt(variance)

	if std == 0.0 {
		return 0.0
	}

	return (mean / std) * math.Sqrt(252.0)
}

// CalculateSortino returns annualized Sortino ratio
func CalculateSortino(returns []float64, riskFreeRate float64) float64 {
	if len(returns) == 0 {
		return 0.0
	}

	n := float64(len(returns))
	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= n

	downsideVar := 0.0
	count := 0.0
	for _, r := range returns {
		if r < riskFreeRate {
			d := r - riskFreeRate
			downsideVar += d * d
			count++
		}
	}

	if count == 0 {
		return 0.0
	}
	downsideVar /= n
	downsideStd := math.Sqrt(downsideVar)

	if downsideStd == 0.0 {
		return 0.0
	}

	return ((mean - riskFreeRate) / downsideStd) * math.Sqrt(252.0)
}

// CalculateMaxDrawdown returns max drawdown as fraction (0.20 = 20%)
func CalculateMaxDrawdown(returns []float64) float64 {
	if len(returns) == 0 {
		return 0.0
	}

	peak := 1.0
	maxDD := 0.0
	cumulative := 1.0

	for _, r := range returns {
		cumulative *= (1.0 + r)
		if cumulative > peak {
			peak = cumulative
		}
		dd := (peak - cumulative) / peak
		if dd > maxDD {
			maxDD = dd
		}
	}

	return maxDD
}

// CalculateWinRate returns proportion of positive trades
func CalculateWinRate(tradePnls []float64) float64 {
	if len(tradePnls) == 0 {
		return 0.0
	}

	wins := 0
	for _, pnl := range tradePnls {
		if pnl > 0.0 {
			wins++
		}
	}

	return float64(wins) / float64(len(tradePnls))
}

// CalculateProfitFactor returns gross profit / gross loss
func CalculateProfitFactor(tradePnls []float64) float64 {
	grossProfit := 0.0
	grossLoss := 0.0

	for _, pnl := range tradePnls {
		if pnl > 0.0 {
			grossProfit += pnl
		} else {
			grossLoss += math.Abs(pnl)
		}
	}

	if grossLoss == 0.0 {
		if grossProfit > 0.0 {
			return 10.0 // cap
		}
		return 0.0
	}

	return grossProfit / grossLoss
}

// ============================================================================
// Monte Carlo Permutation Test
// ============================================================================

// MonteCarloPermutationTest tests if Sharpe ratio is statistically significant
func MonteCarloPermutationTest(returns []float64, nPermutations int) MonteCarloResult {
	if len(returns) == 0 {
		return MonteCarloResult{
			PValue:        1.0,
			IsSignificant: false,
		}
	}

	actualSharpe := CalculateSharpe(returns)

	// Permutation test
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	permuted := make([]float64, len(returns))
	copy(permuted, returns)

	countGE := 0
	sumPermuted := 0.0
	sumSqPermuted := 0.0

	for i := 0; i < nPermutations; i++ {
		rng.Shuffle(len(permuted), func(a, b int) {
			permuted[a], permuted[b] = permuted[b], permuted[a]
		})

		permSharpe := CalculateSharpe(permuted)
		sumPermuted += permSharpe
		sumSqPermuted += permSharpe * permSharpe

		if permSharpe >= actualSharpe {
			countGE++
		}
	}

	meanPermuted := sumPermuted / float64(nPermutations)
	variance := sumSqPermuted/float64(nPermutations) - meanPermuted*meanPermuted
	stdPermuted := math.Sqrt(math.Max(0.0, variance))
	pValue := float64(countGE) / float64(nPermutations)

	return MonteCarloResult{
		ActualSharpe:       actualSharpe,
		MeanPermutedSharpe: meanPermuted,
		StdPermutedSharpe:  stdPermuted,
		PValue:             pValue,
		IsSignificant:      pValue < 0.05,
		NPermutations:      nPermutations,
	}
}

// ============================================================================
// Walk-Forward Validation
// ============================================================================

// ValidateWalkForward runs rolling OOS window validation on returns
func ValidateWalkForward(returns []float64, timestamps []int64, config WalkForwardConfig) *WalkForwardResult {
	start := time.Now()

	result := &WalkForwardResult{
		Windows: make([]WindowResult, 0, config.NWindows),
	}

	if len(returns) < 100 {
		result.CombinedScore = -999.0
		return result
	}

	windowSize := len(returns) / config.NWindows
	trainSize := int(float64(windowSize) * config.TrainRatio)

	var allOOSReturns []float64

	for i := 0; i < config.NWindows; i++ {
		windowStart := i * windowSize
		trainEnd := windowStart + trainSize
		testStart := trainEnd
		testEnd := windowStart + windowSize
		if testEnd > len(returns) {
			testEnd = len(returns)
		}
		if testStart >= testEnd {
			continue
		}

		oosReturns := returns[testStart:testEnd]
		allOOSReturns = append(allOOSReturns, oosReturns...)

		oosSharpe := CalculateSharpe(oosReturns)
		oosMaxDD := CalculateMaxDrawdown(oosReturns) * 100.0 // percentage

		var oosReturn float64
		for _, r := range oosReturns {
			oosReturn += r
		}

		wins := 0
		for _, r := range oosReturns {
			if r > 0.0 {
				wins++
			}
		}
		winRate := float64(wins) / float64(len(oosReturns))

		tsStart := int64(0)
		tsTrainEnd := int64(0)
		tsTestStart := int64(0)
		tsTestEnd := int64(0)
		if len(timestamps) > 0 {
			if windowStart < len(timestamps) {
				tsStart = timestamps[windowStart]
			}
			if trainEnd > 0 && trainEnd-1 < len(timestamps) {
				tsTrainEnd = timestamps[trainEnd-1]
			}
			if testStart < len(timestamps) {
				tsTestStart = timestamps[testStart]
			}
			if testEnd > 0 && testEnd-1 < len(timestamps) {
				tsTestEnd = timestamps[testEnd-1]
			}
		}

		result.Windows = append(result.Windows, WindowResult{
			Index:      i,
			TrainStart: tsStart,
			TrainEnd:   tsTrainEnd,
			TestStart:  tsTestStart,
			TestEnd:    tsTestEnd,
			OSSSharpe:  oosSharpe,
			OOSMaxDD:   oosMaxDD,
			OOSTrades:  len(oosReturns),
			OOSWinRate: winRate,
			OOSReturn:  oosReturn,
		})
	}

	// Aggregate metrics
	nWindows := len(result.Windows)
	if nWindows == 0 {
		result.CombinedScore = -999.0
		return result
	}

	totalSharpe := 0.0
	totalMaxDD := 0.0
	totalTrades := 0
	totalWinRate := 0.0
	totalReturn := 0.0
	profitableWindows := 0
	sharpeValues := make([]float64, 0, nWindows)

	for _, w := range result.Windows {
		totalSharpe += w.OSSSharpe
		totalMaxDD += w.OOSMaxDD
		totalTrades += w.OOSTrades
		totalWinRate += w.OOSWinRate
		totalReturn += w.OOSReturn
		sharpeValues = append(sharpeValues, w.OSSSharpe)
		if w.OOSReturn > 0.0 {
			profitableWindows++
		}
	}

	result.AvgOOSSharpe = totalSharpe / float64(nWindows)
	result.AvgOOSMaxDD = totalMaxDD / float64(nWindows)
	result.TotalOOSTrades = float64(totalTrades)
	result.OOSWinRate = totalWinRate / float64(nWindows)
	result.AvgOOSReturn = totalReturn / float64(nWindows)
	result.ConsistencyScore = float64(profitableWindows) / float64(nWindows)

	// Sharpe stability
	sharpeMean := result.AvgOOSSharpe
	sharpeVar := 0.0
	for _, s := range sharpeValues {
		sharpeVar += (s - sharpeMean) * (s - sharpeMean)
	}
	sharpeVar /= float64(nWindows)
	result.SharpeStability = math.Sqrt(sharpeVar)

	// Monte Carlo
	if config.EnableMonteCarlo && len(allOOSReturns) > 0 {
		mc := MonteCarloPermutationTest(allOOSReturns, config.NPermutations)
		result.MonteCarlo = &mc
	}

	// Combined score
	result.CombinedScore = calculateCombinedScore(
		result.AvgOOSSharpe,
		result.AvgOOSMaxDD,
		totalTrades,
		result.ConsistencyScore,
		0.05, // default p-value threshold
		func() float64 {
			if result.MonteCarlo != nil {
				return result.MonteCarlo.PValue
			}
			return 1.0
		}(),
	)

	result.DurationMs = time.Since(start).Milliseconds()
	return result
}

func calculateCombinedScore(sharpe, maxDD float64, trades int, consistency, pThreshold, pValue float64) float64 {
	// Hard cutoffs
	if trades < 10 {
		return -999.0
	}
	if maxDD > 50.0 {
		return -999.0
	}

	// Base: Sharpe * trade_factor
	tradeFactor := math.Sqrt(math.Min(float64(trades)/50.0, 1.0))
	score := sharpe * tradeFactor

	// Consistency bonus
	score += consistency * 0.5

	// Drawdown penalty
	ddPenalty := math.Max(maxDD-15.0, 0.0) * 2.0
	score -= ddPenalty

	// Statistical significance penalty
	if pValue > pThreshold {
		score *= 0.7
	}

	return score
}

// ============================================================================
// Forced Signal Lag
// ============================================================================

// ApplySignalLag shifts signals by N bars to simulate execution delay
func ApplySignalLag(signals []float64, lag int) []float64 {
	if lag <= 0 || len(signals) <= lag {
		return signals
	}

	lagged := make([]float64, len(signals))
	copy(lagged, signals)

	// Shift signals forward by lag bars
	for i := len(signals) - 1; i >= lag; i-- {
		lagged[i] = signals[i-lag]
	}
	// Zero out the first lag bars (no signal available yet)
	for i := 0; i < lag; i++ {
		lagged[i] = 0.0
	}

	return lagged
}

// ============================================================================
// Extract returns from strategy positions (for validation)
// ============================================================================

// ExtractReturnsFromPositions extracts daily returns from closed positions
func ExtractReturnsFromPositions(positions []PositionData) []float64 {
	if len(positions) == 0 {
		return nil
	}

	// Sort by entry time
	sort.Slice(positions, func(i, j int) bool {
		return positions[i].EntryTime < positions[j].EntryTime
	})

	returns := make([]float64, 0, len(positions))
	for _, pos := range positions {
		if pos.Status != "CLOSED" || pos.EntryPrice == 0.0 {
			continue
		}

		var ret float64
		if pos.Side == Long {
			ret = (pos.ExitPrice - pos.EntryPrice) / pos.EntryPrice
		} else {
			ret = (pos.EntryPrice - pos.ExitPrice) / pos.EntryPrice
		}
		returns = append(returns, ret)
	}

	return returns
}

// ExtractPnlsFromPositions extracts PnLs from closed positions
func ExtractPnlsFromPositions(positions []PositionData) []float64 {
	if len(positions) == 0 {
		return nil
	}

	pnls := make([]float64, 0, len(positions))
	for _, pos := range positions {
		if pos.Status == "CLOSED" {
			pnls = append(pnls, pos.RealizedPnL)
		}
	}

	return pnls
}

// PositionData is a simplified position for validation
type PositionData struct {
	ID           string  `json:"id"`
	Side         string  `json:"side"`
	EntryPrice   float64 `json:"entry_price"`
	ExitPrice    float64 `json:"exit_price"`
	EntryTime    int64   `json:"entry_time"`
	ExitTime     int64   `json:"exit_time"`
	RealizedPnL  float64 `json:"realized_pnl"`
	Status       string  `json:"status"`
}

// Long position constant
const Long = "LONG"
