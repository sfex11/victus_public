package dsl

import (
	"fmt"
	"math"
)

// IndicatorCalculator calculates technical indicators
type IndicatorCalculator struct{}

// NewIndicatorCalculator creates a new calculator
func NewIndicatorCalculator() *IndicatorCalculator {
	return &IndicatorCalculator{}
}

// CalculateAll calculates all indicators for a strategy context
func (ic *IndicatorCalculator) CalculateAll(ctx *EvaluationContext) error {
	if len(ctx.Candles) < 2 {
		return nil
	}

	ctx.Mu.Lock()
	defer ctx.Mu.Unlock()

	// Calculate common EMAs
	for _, period := range []int{5, 10, 20, 50, 100, 200} {
		if len(ctx.Candles) >= period {
			val := ic.CalculateEMA(ctx.Candles, period)
			ctx.Indicators[fmt.Sprintf("ema_%d", period)] = val
			ctx.Indicators[fmt.Sprintf("ema(%d)", period)] = val
		}
	}

	// Calculate common SMAs
	for _, period := range []int{5, 10, 20, 50, 100, 200} {
		if len(ctx.Candles) >= period {
			val := ic.CalculateSMA(ctx.Candles, period)
			ctx.Indicators[fmt.Sprintf("sma_%d", period)] = val
			ctx.Indicators[fmt.Sprintf("sma(%d)", period)] = val
		}
	}

	// Calculate RSI
	for _, period := range []int{14, 21} {
		if len(ctx.Candles) >= period+1 {
			val := ic.CalculateRSI(ctx.Candles, period)
			ctx.Indicators[fmt.Sprintf("rsi_%d", period)] = val
			ctx.Indicators[fmt.Sprintf("rsi(%d)", period)] = val
		}
	}

	// Calculate Bollinger Bands
	if len(ctx.Candles) >= 20 {
		middle := ic.CalculateSMA(ctx.Candles, 20)
		std := ic.CalculateStdDev(ctx.Candles, 20)
		ctx.Indicators["bb_upper"] = middle + 2*std
		ctx.Indicators["bb_middle"] = middle
		ctx.Indicators["bb_lower"] = middle - 2*std
		ctx.Indicators["bb_width"] = 4 * std
	}

	// Calculate ATR
	if len(ctx.Candles) >= 14 {
		ctx.Indicators["atr_14"] = ic.CalculateATR(ctx.Candles, 14)
	}

	// Price momentum
	if len(ctx.Candles) >= 2 {
		ctx.Indicators["momentum_1"] = (ctx.Candles[len(ctx.Candles)-1].Close - ctx.Candles[len(ctx.Candles)-2].Close) / ctx.Candles[len(ctx.Candles)-2].Close * 100
	}
	if len(ctx.Candles) >= 5 {
		ctx.Indicators["momentum_5"] = (ctx.Candles[len(ctx.Candles)-1].Close - ctx.Candles[len(ctx.Candles)-5].Close) / ctx.Candles[len(ctx.Candles)-5].Close * 100
	}

	// Volatility
	if len(ctx.Candles) >= 20 {
		ctx.Indicators["volatility"] = ic.CalculateVolatility(ctx.Candles, 20)
	}

	// High/Low
	if len(ctx.Candles) >= 1 {
		high := ctx.Candles[len(ctx.Candles)-1].High
		low := ctx.Candles[len(ctx.Candles)-1].Low
		ctx.Indicators["high"] = high
		ctx.Indicators["low"] = low
		ctx.Indicators["range"] = high - low
		ctx.Indicators["range_pct"] = (high - low) / low * 100
	}

	return nil
}

// CalculateEMA calculates Exponential Moving Average
func (ic *IndicatorCalculator) CalculateEMA(candles []*Candle, period int) float64 {
	if len(candles) < period {
		return 0
	}

	// Start with SMA for first value
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += candles[i].Close
	}
	ema := sum / float64(period)

	// Calculate EMA for remaining candles
	multiplier := 2.0 / (float64(period) + 1.0)
	for i := period; i < len(candles); i++ {
		ema = (candles[i].Close - ema)*multiplier + ema
	}

	return ema
}

// CalculateSMA calculates Simple Moving Average
func (ic *IndicatorCalculator) CalculateSMA(candles []*Candle, period int) float64 {
	if len(candles) < period {
		return 0
	}

	sum := 0.0
	start := len(candles) - period
	for i := start; i < len(candles); i++ {
		sum += candles[i].Close
	}

	return sum / float64(period)
}

// CalculateRSI calculates Relative Strength Index
func (ic *IndicatorCalculator) CalculateRSI(candles []*Candle, period int) float64 {
	if len(candles) < period+1 {
		return 50.0 // Neutral RSI
	}

	gains := make([]float64, 0, period)
	losses := make([]float64, 0, period)

	for i := len(candles) - period; i < len(candles); i++ {
		change := candles[i].Close - candles[i-1].Close
		if change > 0 {
			gains = append(gains, change)
			losses = append(losses, 0)
		} else {
			gains = append(gains, 0)
			losses = append(losses, -change)
		}
	}

	avgGain := 0.0
	avgLoss := 0.0
	for _, g := range gains {
		avgGain += g
	}
	for _, l := range losses {
		avgLoss += l
	}
	avgGain /= float64(period)
	avgLoss /= float64(period)

	if avgLoss == 0 {
		return 100.0
	}

	rs := avgGain / avgLoss
	rsi := 100 - (100 / (1 + rs))

	return rsi
}

// CalculateStdDev calculates Standard Deviation
func (ic *IndicatorCalculator) CalculateStdDev(candles []*Candle, period int) float64 {
	if len(candles) < period {
		return 0
	}

	sma := ic.CalculateSMA(candles, period)

	sumSquares := 0.0
	start := len(candles) - period
	for i := start; i < len(candles); i++ {
		diff := candles[i].Close - sma
		sumSquares += diff * diff
	}

	variance := sumSquares / float64(period)
	return math.Sqrt(variance)
}

// CalculateATR calculates Average True Range
func (ic *IndicatorCalculator) CalculateATR(candles []*Candle, period int) float64 {
	if len(candles) < period+1 {
		return 0
	}

	trueRanges := make([]float64, 0, period)
	for i := len(candles) - period; i < len(candles); i++ {
		high := candles[i].High
		low := candles[i].Low
		prevClose := candles[i-1].Close

		tr := high - low
		if hPrevClose := high - prevClose; hPrevClose > tr {
			tr = hPrevClose
		}
		if lPrevClose := low - prevClose; lPrevClose > tr {
			tr = lPrevClose
		}

		trueRanges = append(trueRanges, tr)
	}

	sum := 0.0
	for _, tr := range trueRanges {
		sum += tr
	}

	return sum / float64(period)
}

// CalculateVolatility calculates price volatility
func (ic *IndicatorCalculator) CalculateVolatility(candles []*Candle, period int) float64 {
	if len(candles) < period {
		return 0
	}

	returns := make([]float64, 0, period-1)
	start := len(candles) - period
	for i := start + 1; i < len(candles); i++ {
		ret := (candles[i].Close - candles[i-1].Close) / candles[i-1].Close
		returns = append(returns, ret)
	}

	// Calculate standard deviation of returns
	sum := 0.0
	for _, r := range returns {
		sum += r
	}
	mean := sum / float64(len(returns))

	sumSquares := 0.0
	for _, r := range returns {
		diff := r - mean
		sumSquares += diff * diff
	}

	variance := sumSquares / float64(len(returns))
	return math.Sqrt(variance) * 100 // As percentage
}

// CalculateMACD calculates MACD indicator
func (ic *IndicatorCalculator) CalculateMACD(candles []*Candle) (macd, signal, histogram float64) {
	if len(candles) < 26 {
		return 0, 0, 0
	}

	ema12 := ic.CalculateEMA(candles, 12)
	ema26 := ic.CalculateEMA(candles, 26)

	macd = ema12 - ema26

	// For signal, we'd need historical MACD values
	// Simplified: use EMA of recent prices as proxy
	signal = ic.CalculateEMA(candles, 9)
	histogram = macd - signal

	return macd, signal, histogram
}
