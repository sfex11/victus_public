package ml

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"dsl-strategy-evolver/internal/data"
)

// WalkForwardConfig configures the walk-forward backtest
type WalkForwardConfig struct {
	TrainWindowDays int     // training window size (default 30)
	TestWindowDays  int     // out-of-sample test window (default 7)
	StepDays        int     // step size between folds (default 7)
	MinTrainSamples int     // minimum samples to train (default 500)
	InitialCapital  float64 // starting capital (default 10000)
	CommissionPct   float64 // trading commission (default 0.04)
}

// DefaultWalkForwardConfig returns sensible defaults
func DefaultWalkForwardConfig() *WalkForwardConfig {
	return &WalkForwardConfig{
		TrainWindowDays: 30,
		TestWindowDays:  7,
		StepDays:        7,
		MinTrainSamples: 500,
		InitialCapital:  10000,
		CommissionPct:   0.04,
	}
}

// WalkForwardResult holds the results of a walk-forward backtest
type WalkForwardResult struct {
	Symbol        string    `json:"symbol"`
	TotalFolds    int       `json:"total_folds"`
	SuccessfulFolds int     `json:"successful_folds"`
	SharpeRatio   float64   `json:"sharpe_ratio"`
	MaxDrawdown   float64   `json:"max_drawdown"`
	WinRate       float64   `json:"win_rate"`
	TotalReturn   float64   `json:"total_return"`
	FoldResults   []FoldResult `json:"fold_results"`
	EvaluatedAt   time.Time  `json:"evaluated_at"`
}

// FoldResult is a single fold's result
type FoldResult struct {
	FoldNum       int       `json:"fold_num"`
	TrainStart    time.Time `json:"train_start"`
	TrainEnd      time.Time `json:"train_end"`
	TestStart     time.Time `json:"test_start"`
	TestEnd       time.Time `json:"test_end"`
	TrainSamples  int       `json:"train_samples"`
	TestSamples   int       `json:"test_samples"`
	FoldReturn    float64   `json:"fold_return"`
	FoldSharpe    float64   `json:"fold_sharpe"`
	FoldWinRate   float64   `json:"fold_win_rate"`
	MaxDrawdown   float64   `json:"max_drawdown"`
}

// WalkForwardBacktest runs a walk-forward backtest using ML predictions
func WalkForwardBacktest(
	symbol string,
	features []*FeatureSet,
	config *WalkForwardConfig,
) (*WalkForwardResult, error) {
	if config == nil {
		config = DefaultWalkForwardConfig()
	}
	if len(features) == 0 {
		return nil, fmt.Errorf("no feature data")
	}

	// Sort features by timestamp
	sort.Slice(features, func(i, j int) bool {
		return features[i].Timestamp < features[j].Timestamp
	})

	// Convert windows to hours
	trainWindowHours := config.TrainWindowDays * 24
	testWindowHours := config.TestWindowDays * 24
	stepHours := config.StepDays * 24

	// Calculate time range
	startTime := features[0].Timestamp
	endTime := features[len(features)-1].Timestamp
	totalHours := endTime - startTime

	result := &WalkForwardResult{
		Symbol:      symbol,
		EvaluatedAt: time.Now(),
	}

	// Slide through time
	currentOffset := 0
	foldNum := 0
	var allReturns []float64
	var equityCurve []float64
	equity := config.InitialCapital

	for currentOffset+trainWindowHours+testWindowHours <= int(totalHours) {
		foldNum++
		trainStart := startTime + int64(currentOffset)
		trainEnd := trainStart + int64(trainWindowHours)
		testStart := trainEnd
		testEnd := testStart + int64(testWindowHours)

		// Split features
		trainFeatures, testFeatures := splitByTime(features, trainStart, trainEnd, testStart, testEnd)

		if len(trainFeatures) < config.MinTrainSamples {
			currentOffset += int(stepHours)
			continue
		}

		// Simple prediction: use mean of training targets as baseline
		// In production, this would call the Python LightGBM service
		meanTarget := calcMeanTarget(trainFeatures)
		threshold := 0.1 // % return threshold for signal

		// Simulate trades on test set
		var foldReturns []float64
		foldEquity := config.InitialCapital

		for _, tf := range testFeatures {
			predictedReturn := meanTarget // Simplified prediction
			actualReturn := tf.Target

			// Commission
			if abs(predictedReturn) > threshold {
				commission := config.CommissionPct / 100.0 * 2 // round trip
				strategyReturn := actualReturn - commission
				foldEquity *= (1 + strategyReturn/100.0)
				foldReturns = append(foldReturns, strategyReturn)
			} else {
				foldReturns = append(foldReturns, 0)
			}
		}

		// Compute fold metrics
		foldResult := FoldResult{
			FoldNum:      foldNum,
			TrainStart:   time.Unix(trainStart, 0),
			TrainEnd:     time.Unix(trainEnd, 0),
			TestStart:    time.Unix(testStart, 0),
			TestEnd:      time.Unix(testEnd, 0),
			TrainSamples: len(trainFeatures),
			TestSamples:  len(testFeatures),
			FoldReturn:   (foldEquity/config.InitialCapital - 1) * 100,
		}

		if len(foldReturns) > 1 {
			foldResult.FoldSharpe = calcSharpeFromReturns(foldReturns)
			foldResult.FoldWinRate = calcWinRateFromReturns(foldReturns)
			foldResult.MaxDrawdown = calcMaxDrawdown(foldReturns)
		}

		result.FoldResults = append(result.FoldResults, foldResult)
		result.SuccessfulFolds++
		allReturns = append(allReturns, foldReturns...)
		equity = foldEquity
		equityCurve = append(equityCurve, foldEquity)

		currentOffset += int(stepHours)
	}

	result.TotalFolds = foldNum

	if len(allReturns) > 1 {
		result.SharpeRatio = calcSharpeFromReturns(allReturns)
		result.WinRate = calcWinRateFromReturns(allReturns)
		result.MaxDrawdown = calcMaxDrawdown(allReturns)
		result.TotalReturn = (equity/config.InitialCapital - 1) * 100
	}

	return result, nil
}

// ExportTrainingData exports feature sets as JSONL for the Python ML service
func ExportTrainingData(features []*FeatureSet, outputPath string) error {
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, fs := range features {
		record := map[string]interface{}{
			"symbol":   fs.Symbol,
			"timestamp": fs.Timestamp,
			"features": fs.Features,
			"target":   fs.Target,
		}
		if err := enc.Encode(record); err != nil {
			return fmt.Errorf("encode record: %w", err)
		}
	}

	return nil
}

// --- Helper functions ---

func splitByTime(features []*FeatureSet, trainStart, trainEnd, testStart, testEnd int64) (train, test []*FeatureSet) {
	for _, f := range features {
		if f.Timestamp >= trainStart && f.Timestamp <= trainEnd {
			train = append(train, f)
		}
		if f.Timestamp > testStart && f.Timestamp <= testEnd {
			test = append(test, f)
		}
	}
	return
}

func calcMeanTarget(features []*FeatureSet) float64 {
	sum := 0.0
	for _, f := range features {
		sum += f.Target
	}
	if len(features) == 0 {
		return 0
	}
	return sum / float64(len(features))
}

func calcSharpeFromReturns(returns []float64) float64 {
	if len(returns) < 2 {
		return 0
	}

	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))

	variance := 0.0
	for _, r := range returns {
		diff := r - mean
		variance += diff * diff
	}
	variance /= float64(len(returns))
	std := math.Sqrt(variance)

	if std == 0 {
		return 0
	}

	// Annualize assuming hourly returns
	sharpe := (mean / std) * math.Sqrt(8760)
	return math.Round(sharpe*10000) / 10000
}

func calcWinRateFromReturns(returns []float64) float64 {
	if len(returns) == 0 {
		return 0
	}

	wins := 0
	for _, r := range returns {
		if r > 0 {
			wins++
		}
	}
	return math.Round(float64(wins)/float64(len(returns))*10000) / 10000
}

func calcMaxDrawdown(returns []float64) float64 {
	equity := 1.0
	peak := 1.0
	maxDD := 0.0

	for _, r := range returns {
		equity *= (1 + r/100.0)
		if equity > peak {
			peak = equity
		}
		dd := (peak - equity) / peak * 100
		if dd > maxDD {
			maxDD = dd
		}
	}

	return math.Round(maxDD*100) / 100
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// Mock candle data for testing when DB is not available
func MockCandles(count int) []*data.Candle {
	candles := make([]*data.Candle, count)
	price := 50000.0
	baseTime := time.Now().Add(-time.Duration(count) * time.Hour).Unix()

	for i := 0; i < count; i++ {
		change := (0.5 - randFloat()) * 1000
		price += change
		if price < 10000 {
			price = 10000
		}

		vol := 100 + randFloat()*900

		candles[i] = &data.Candle{
			Symbol:    "BTCUSDT",
			Open:      price - change/2,
			High:      price + randFloat()*500,
			Low:       price - randFloat()*500,
			Close:     price,
			Volume:    vol,
			Timestamp: baseTime + int64(i*3600),
		}
	}
	return candles
}

func randFloat() float64 {
	// Simple deterministic pseudo-random for reproducibility
	return 0.42 // placeholder
}
