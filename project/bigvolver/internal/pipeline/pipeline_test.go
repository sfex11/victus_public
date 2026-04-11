package pipeline

import (
	"math"
	"testing"
)

// ============================================================
// WeightVector Tests
// ============================================================

func TestNewWeightVector(t *testing.T) {
	wv := NewWeightVector(1712800000)
	if wv.Timestamp != 1712800000 {
		t.Errorf("expected timestamp 1712800000, got %d", wv.Timestamp)
	}
	if len(wv.Weights) != 0 {
		t.Errorf("expected empty weights, got %d", len(wv.Weights))
	}
}

func TestAddWeight(t *testing.T) {
	wv := NewWeightVector(0)
	wv.AddWeight(Weight{Symbol: "BTCUSDT", Weight: 0.5, Confidence: 0.8})
	wv.AddWeight(Weight{Symbol: "ETHUSDT", Weight: -0.3, Confidence: 0.6})

	if len(wv.Weights) != 2 {
		t.Fatalf("expected 2 weights, got %d", len(wv.Weights))
	}
	if wv.Weights[0].Symbol != "BTCUSDT" {
		t.Errorf("expected BTCUSDT, got %s", wv.Weights[0].Symbol)
	}
}

func TestGetWeight(t *testing.T) {
	wv := NewWeightVector(0)
	wv.AddWeight(Weight{Symbol: "BTCUSDT", Weight: 0.5})
	wv.AddWeight(Weight{Symbol: "ETHUSDT", Weight: -0.3})

	w, ok := wv.GetWeight("BTCUSDT")
	if !ok || w.Weight != 0.5 {
		t.Errorf("expected BTCUSDT weight 0.5, got ok=%v weight=%.2f", ok, w.Weight)
	}

	_, ok = wv.GetWeight("SOLUSDT")
	if ok {
		t.Error("expected false for non-existent symbol")
	}
}

func TestNormalizeWeights(t *testing.T) {
	wv := NewWeightVector(0)
	wv.AddWeight(Weight{Symbol: "BTCUSDT", Weight: 0.6})
	wv.AddWeight(Weight{Symbol: "ETHUSDT", Weight: -0.4})

	wv.NormalizeWeights()

	// abs sum = 0.6 + 0.4 = 1.0, should stay the same
	if math.Abs(wv.Weights[0].Weight-0.6) > 0.0001 {
		t.Errorf("BTCUSDT should be 0.6, got %.4f", wv.Weights[0].Weight)
	}
	if math.Abs(wv.Weights[1].Weight+0.4) > 0.0001 {
		t.Errorf("ETHUSDT should be -0.4, got %.4f", wv.Weights[1].Weight)
	}
}

func TestNormalizeWeightsUneven(t *testing.T) {
	wv := NewWeightVector(0)
	wv.AddWeight(Weight{Symbol: "BTCUSDT", Weight: 0.3})
	wv.AddWeight(Weight{Symbol: "ETHUSDT", Weight: -0.1})
	wv.AddWeight(Weight{Symbol: "SOLUSDT", Weight: 0.2})

	wv.NormalizeWeights()

	// abs sum = 0.3 + 0.1 + 0.2 = 0.6
	// BTCUSDT: 0.3/0.6 = 0.5
	// ETHUSDT: -0.1/0.6 = -0.1667
	// SOLUSDT: 0.2/0.6 = 0.3333
	if math.Abs(wv.Weights[0].Weight-0.5) > 0.0001 {
		t.Errorf("BTCUSDT should be 0.5, got %.4f", wv.Weights[0].Weight)
	}
	if math.Abs(wv.Weights[1].Weight+0.1667) > 0.001 {
		t.Errorf("ETHUSDT should be -0.1667, got %.4f", wv.Weights[1].Weight)
	}
}

func TestNormalizeZeroWeights(t *testing.T) {
	wv := NewWeightVector(0)
	wv.AddWeight(Weight{Symbol: "BTCUSDT", Weight: 0})

	// Should not divide by zero
	wv.NormalizeWeights()

	if wv.Weights[0].Weight != 0 {
		t.Errorf("expected 0, got %.4f", wv.Weights[0].Weight)
	}
}

func TestFilterByThreshold(t *testing.T) {
	wv := NewWeightVector(0)
	wv.AddWeight(Weight{Symbol: "BTCUSDT", Weight: 0.5, Confidence: 0.8})
	wv.AddWeight(Weight{Symbol: "ETHUSDT", Weight: -0.3, Confidence: 0.4})
	wv.AddWeight(Weight{Symbol: "SOLUSDT", Weight: 0.1, Confidence: 0.2})

	wv.FilterByThreshold(0.3)

	if len(wv.Weights) != 2 {
		t.Errorf("expected 2 weights after filtering, got %d", len(wv.Weights))
	}
}

func TestTotalExposure(t *testing.T) {
	wv := NewWeightVector(0)
	wv.AddWeight(Weight{Symbol: "BTCUSDT", Weight: 0.5})
	wv.AddWeight(Weight{Symbol: "ETHUSDT", Weight: -0.3})

	exp := wv.TotalExposure()
	if math.Abs(exp-0.8) > 0.0001 {
		t.Errorf("expected 0.8, got %.4f", exp)
	}
}

func TestNetExposure(t *testing.T) {
	wv := NewWeightVector(0)
	wv.AddWeight(Weight{Symbol: "BTCUSDT", Weight: 0.5})
	wv.AddWeight(Weight{Symbol: "ETHUSDT", Weight: -0.3})

	net := wv.NetExposure()
	if math.Abs(net-0.2) > 0.0001 {
		t.Errorf("expected 0.2, got %.4f", net)
	}
}

func TestLongShortCount(t *testing.T) {
	wv := NewWeightVector(0)
	wv.AddWeight(Weight{Symbol: "BTCUSDT", Weight: 0.5})
	wv.AddWeight(Weight{Symbol: "ETHUSDT", Weight: -0.3})
	wv.AddWeight(Weight{Symbol: "SOLUSDT", Weight: 0.2})
	wv.AddWeight(Weight{Symbol: "DOGEUSDT", Weight: -0.1})

	if wv.LongCount() != 2 {
		t.Errorf("expected 2 longs, got %d", wv.LongCount())
	}
	if wv.ShortCount() != 2 {
		t.Errorf("expected 2 shorts, got %d", wv.ShortCount())
	}
}

// ============================================================
// PipelineChain Tests
// ============================================================

// doublerStage doubles all weights
type doublerStage struct {
	name string
}

func (m *doublerStage) Name() string { return m.name }
func (m *doublerStage) Process(wv *WeightVector) (*WeightVector, error) {
	for i := range wv.Weights {
		wv.Weights[i].Weight *= 2.0
	}
	return wv, nil
}

// errorStage always returns an error
type errorStage struct{}

func (e *errorStage) Name() string                          { return "error_stage" }
func (e *errorStage) Process(wv *WeightVector) (*WeightVector, error) {
	return nil, &testErr{"forced error"}
}

type testErr struct{ msg string }

func (e *testErr) Error() string { return e.msg }

func TestPipelineChain(t *testing.T) {
	wv := NewWeightVector(0)
	wv.AddWeight(Weight{Symbol: "BTCUSDT", Weight: 0.3})

	chain := NewPipelineChain(&doublerStage{name: "doubler_1"}, &doublerStage{name: "doubler_2"})
	result, err := chain.Process(wv)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 0.3 * 2 * 2 = 1.2
	if math.Abs(result.Weights[0].Weight-1.2) > 0.0001 {
		t.Errorf("expected 1.2, got %.4f", result.Weights[0].Weight)
	}

	stages := chain.Stages()
	if len(stages) != 2 || stages[0] != "doubler_1" || stages[1] != "doubler_2" {
		t.Errorf("unexpected stages: %v", stages)
	}
}

func TestPipelineChainSingle(t *testing.T) {
	wv := NewWeightVector(0)
	wv.AddWeight(Weight{Symbol: "BTCUSDT", Weight: 0.3})

	chain := NewPipelineChain()
	result, err := chain.Process(wv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if math.Abs(result.Weights[0].Weight-0.3) > 0.0001 {
		t.Errorf("empty chain should pass through, got %.4f", result.Weights[0].Weight)
	}
}

func TestPipelineChainError(t *testing.T) {
	wv := NewWeightVector(0)
	wv.AddWeight(Weight{Symbol: "BTCUSDT", Weight: 0.3})

	chain := NewPipelineChain(&doublerStage{name: "ok"}, &errorStage{})
	_, err := chain.Process(wv)

	if err == nil {
		t.Fatal("expected error from error stage")
	}
}

// ============================================================
// RiskOverlay Tests
// ============================================================

func TestPositionSizeLimit(t *testing.T) {
	ro := NewRiskOverlay()

	wv := NewWeightVector(0)
	wv.AddWeight(Weight{Symbol: "BTCUSDT", Weight: 0.8})
	wv.AddWeight(Weight{Symbol: "ETHUSDT", Weight: -0.5})

	result, err := ro.Process(wv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Max position size is 0.3 (before normalize)
	if result.Weights[0].Weight <= 0 || result.Weights[1].Weight >= 0 {
		t.Errorf("BTCUSDT should be positive, ETHUSDT should be negative")
	}
	// After normalize, both should be within [-0.3, 0.3] equivalent
}

func TestTotalExposureLimit(t *testing.T) {
	ro := NewRiskOverlay()

	wv := NewWeightVector(0)
	wv.AddWeight(Weight{Symbol: "BTCUSDT", Weight: 0.25})
	wv.AddWeight(Weight{Symbol: "ETHUSDT", Weight: 0.25})
	wv.AddWeight(Weight{Symbol: "SOLUSDT", Weight: 0.25})
	wv.AddWeight(Weight{Symbol: "DOGEUSDT", Weight: 0.25})
	wv.AddWeight(Weight{Symbol: "ADAUSDT", Weight: 0.25})

	// Total exposure = 1.25, should be scaled to 1.0
	result, err := ro.Process(wv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	exp := result.TotalExposure()
	if exp > 1.0+0.01 {
		t.Errorf("total exposure should be <= 1.0, got %.4f", exp)
	}
}

func TestNetExposureLimit(t *testing.T) {
	ro := NewRiskOverlay()

	wv := NewWeightVector(0)
	wv.AddWeight(Weight{Symbol: "BTCUSDT", Weight: 0.25})
	wv.AddWeight(Weight{Symbol: "ETHUSDT", Weight: 0.25})
	wv.AddWeight(Weight{Symbol: "SOLUSDT", Weight: 0.25})

	// Net = 0.75, max = 0.5 → should be reduced
	result, err := ro.Process(wv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	net := result.NetExposure()
	if net > 0.5+0.01 {
		t.Errorf("net exposure should be <= 0.5, got %.4f", net)
	}
}

func TestHighVolReduction(t *testing.T) {
	ro := NewRiskOverlay()

	wv := NewWeightVector(0)
	wv.AddWeight(Weight{
		Symbol: "BTCUSDT", Weight: 0.3,
		Metadata: map[string]interface{}{"atr_14": 500.0},
	})
	wv.AddWeight(Weight{
		Symbol: "ETHUSDT", Weight: 0.3,
		Metadata: map[string]interface{}{"atr_14": 100.0},
	})

	// BTC ATR (500) > avg ATR (300) * 2.0 → should be reduced
	result, err := ro.Process(wv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// BTC weight was halved (0.15), ETH stayed (0.3)
	// After normalize: BTC has less weight
	if result.Weights[0].Weight >= result.Weights[1].Weight {
		t.Errorf("high-vol BTC should have less weight than ETH")
	}
}

func TestMaxDrawdownReduction(t *testing.T) {
	ro := NewRiskOverlay(WithStartingEquity(10000))
	ro.UpdateEquity(9000) // 10% DD — under 15% threshold
	ro.UpdateEquity(8000) // 20% DD — over 15%, should halve

	wv := NewWeightVector(0)
	wv.AddWeight(Weight{Symbol: "BTCUSDT", Weight: 0.3})

	result, err := ro.Process(wv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Weight should be halved: 0.3 * 0.5 = 0.15, then normalized
	if result.Weights[0].Weight <= 0 || result.Weights[0].Weight > 0.16 {
		t.Errorf("expected ~0.15 after DD halving, got %.4f", result.Weights[0].Weight)
	}
}

func TestPortfolioRiskScore(t *testing.T) {
	ro := NewRiskOverlay()

	// Concentrated portfolio (higher risk)
	wv1 := NewWeightVector(0)
	wv1.AddWeight(Weight{Symbol: "BTCUSDT", Weight: 0.9})
	result1, _ := ro.Process(wv1)

	// Diversified portfolio (lower risk)
	wv2 := NewWeightVector(0)
	wv2.AddWeight(Weight{Symbol: "BTCUSDT", Weight: 0.25})
	wv2.AddWeight(Weight{Symbol: "ETHUSDT", Weight: 0.25})
	wv2.AddWeight(Weight{Symbol: "SOLUSDT", Weight: 0.25})
	wv2.AddWeight(Weight{Symbol: "DOGEUSDT", Weight: 0.25})
	result2, _ := ro.Process(wv2)

	if result1.TotalRisk <= result2.TotalRisk {
		t.Errorf("concentrated portfolio should have higher risk: %.4f vs %.4f",
			result1.TotalRisk, result2.TotalRisk)
	}
}

func TestCurrentDrawdown(t *testing.T) {
	ro := NewRiskOverlay(WithStartingEquity(10000))

	dd := ro.CurrentDrawdown()
	if dd != 0 {
		t.Errorf("initial DD should be 0, got %.2f", dd)
	}

	ro.UpdateEquity(8500)
	dd = ro.CurrentDrawdown()
	if math.Abs(dd-15.0) > 0.01 {
		t.Errorf("DD should be 15%%, got %.2f", dd)
	}

	ro.UpdateEquity(11000) // new peak
	ro.UpdateEquity(9900)
	dd = ro.CurrentDrawdown()
	if math.Abs(dd-10.0) > 0.01 {
		t.Errorf("DD should be 10%% (from 11000 peak), got %.2f", dd)
	}
}

// ============================================================
// Full Pipeline Integration Test
// ============================================================

func TestFullPipelineChain(t *testing.T) {
	// Simulate: MLSelector output → Timing → RiskOverlay
	// Since MLSelector and Timing need DB, test with weight-only stages

	wv := NewWeightVector(0)
	wv.AddWeight(Weight{Symbol: "BTCUSDT", Weight: 0.6, Confidence: 0.9, Signal: "LONG"})
	wv.AddWeight(Weight{Symbol: "ETHUSDT", Weight: -0.4, Confidence: 0.7, Signal: "SHORT"})
	wv.AddWeight(Weight{Symbol: "SOLUSDT", Weight: 0.2, Confidence: 0.4, Signal: "LONG"})

	ro := NewRiskOverlay(WithStartingEquity(10000))

	chain := NewPipelineChain(ro)
	result, err := chain.Process(wv)

	if err != nil {
		t.Fatalf("pipeline error: %v", err)
	}

	// Verify constraints
	if result.TotalExposure() > 1.0+0.01 {
		t.Errorf("total exposure exceeded: %.4f", result.TotalExposure())
	}
	if math.Abs(result.NetExposure()) > 0.5+0.01 {
		t.Errorf("net exposure exceeded: %.4f", result.NetExposure())
	}
	if result.TotalRisk < 0 || result.TotalRisk > 1 {
		t.Errorf("risk score out of range: %.4f", result.TotalRisk)
	}
	// RawScore should be set before risk adjustments
	if result.RawScore < 0.5 {
		t.Errorf("raw score too low: %.4f", result.RawScore)
	}
}
