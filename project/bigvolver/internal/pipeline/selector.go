package pipeline

import (
	"fmt"
	"math"
	"sort"

	"dsl-strategy-evolver/internal/ml"
)

// MLSelector converts ML predictions into portfolio weights
type MLSelector struct {
	predictor     *ml.Predictor
	dataWindow    *ml.DataWindow
	featurePipeline *ml.FeaturePipeline
	maxPositions  int
	minConfidence float64
}

// NewMLSelector creates a new ML-based symbol selector
func NewMLSelector(
	predictor *ml.Predictor,
	dw *ml.DataWindow,
	fp *ml.FeaturePipeline,
	opts ...SelectorOption,
) *MLSelector {
	s := &MLSelector{
		predictor:      predictor,
		dataWindow:     dw,
		featurePipeline: fp,
		maxPositions:   5,
		minConfidence:  0.3,
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// SelectorOption configures the ML selector
type SelectorOption func(*MLSelector)

// WithMaxPositions sets the maximum number of positions
func WithMaxPositions(n int) SelectorOption {
	return func(s *MLSelector) { s.maxPositions = n }
}

// WithMinConfidence sets the minimum confidence threshold
func WithMinConfidence(c float64) SelectorOption {
	return func(s *MLSelector) { s.minConfidence = c }
}

// SelectSymbols runs ML predictions for each symbol and returns a weight vector
func (s *MLSelector) SelectSymbols(symbols []string) (*WeightVector, error) {
	wv := NewWeightVector(ml.CurrentTimestamp())

	type scored struct {
		symbol     string
		weight     float64
		confidence float64
		signal     string
	}

	var scores []scored

	for _, symbol := range symbols {
		// 1. Compute latest features
		features, err := s.featurePipeline.ComputeLatestFeatures(symbol)
		if err != nil {
			continue // Skip symbols with insufficient data
		}

		// 2. Get ML prediction
		pred, err := s.predictor.Predict(symbol, features.Features)
		if err != nil {
			continue // Skip prediction failures
		}

		// 3. Check minimum confidence
		if pred.Confidence < s.minConfidence {
			continue
		}

		// 4. Convert to weight
		var weight float64
		switch pred.Signal {
		case "LONG":
			weight = pred.Confidence
		case "SHORT":
			weight = -pred.Confidence
		default:
			continue // Skip NEUTRAL
		}

		scores = append(scores, scored{
			symbol:     symbol,
			weight:     weight,
			confidence: pred.Confidence,
			signal:     pred.Signal,
		})
	}

	// 5. Sort by confidence (descending)
	sort.Slice(scores, func(i, j int) bool {
		return math.Abs(scores[i].weight) > math.Abs(scores[j].weight)
	})

	// 6. Take top N
	limit := len(scores)
	if limit > s.maxPositions {
		limit = s.maxPositions
	}

	for i := 0; i < limit; i++ {
		sc := scores[i]
		wv.AddWeight(Weight{
			Symbol:    sc.symbol,
			Weight:    sc.weight,
			Confidence: sc.confidence,
			Signal:    sc.signal,
			Source:    "ml_selector",
			Metadata: map[string]interface{}{
				"predicted_return": 0, // will be filled by predictor
			},
		})
	}

	// 7. Normalize
	wv.NormalizeWeights()
	wv.RawScore = wv.TotalExposure()

	return wv, nil
}

// Name implements PipelineStage
func (s *MLSelector) Name() string {
	return "ml_selector"
}

// Process implements PipelineStage — runs ML selection on available symbols
func (s *MLSelector) Process(wv *WeightVector) (*WeightVector, error) {
	// Extract symbols from existing weights (or use all available)
	symbols := make([]string, 0, len(wv.Weights))
	for _, w := range wv.Weights {
		symbols = append(symbols, w.Symbol)
	}

	if len(symbols) == 0 {
		return wv, fmt.Errorf("no symbols to select from")
	}

	newWV, err := s.SelectSymbols(symbols)
	if err != nil {
		return wv, err // Fail-safe: return original
	}

	return newWV, nil
}
