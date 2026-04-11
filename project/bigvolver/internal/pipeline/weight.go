package pipeline

// Weight represents the allocated weight for a single symbol/asset
type Weight struct {
	Symbol    string  `json:"symbol"`
	Weight    float64 `json:"weight"`     // -1.0 to 1.0 (negative = short)
	Confidence float64 `json:"confidence"` // 0.0 to 1.0
	Signal    string  `json:"signal"`     // "LONG", "SHORT", "NEUTRAL"
	Source    string  `json:"source"`     // which module produced this: "ml_selector", "drl", "manual"
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// WeightVector is the full portfolio allocation at a point in time
type WeightVector struct {
	Timestamp int64     `json:"timestamp"`
	Weights   []Weight  `json:"weights"`
	TotalRisk float64   `json:"total_risk"`   // portfolio-level risk metric (0-1)
	RawScore  float64   `json:"raw_score"`    // before risk overlay
	FinalScore float64  `json:"final_score"`  // after risk overlay
}

// NewWeightVector creates an empty weight vector
func NewWeightVector(timestamp int64) *WeightVector {
	return &WeightVector{
		Timestamp: timestamp,
		Weights:   make([]Weight, 0),
	}
}

// AddWeight appends a weight to the vector
func (wv *WeightVector) AddWeight(w Weight) {
	wv.Weights = append(wv.Weights, w)
}

// GetWeight returns the weight for a specific symbol
func (wv *WeightVector) GetWeight(symbol string) (Weight, bool) {
	for _, w := range wv.Weights {
		if w.Symbol == symbol {
			return w, true
		}
	}
	return Weight{}, false
}

// NormalizeWeights normalizes all weights so their absolute sum equals 1.0
// This ensures the portfolio is fully allocated (or de-allocated)
func (wv *WeightVector) NormalizeWeights() {
	var absSum float64
	for _, w := range wv.Weights {
		absSum += abs64(w.Weight)
	}
	if absSum == 0 {
		return
	}
	for i := range wv.Weights {
		wv.Weights[i].Weight /= absSum
	}
}

// FilterByThreshold removes weights below a minimum confidence threshold
func (wv *WeightVector) FilterByThreshold(minConfidence float64) {
	filtered := make([]Weight, 0, len(wv.Weights))
	for _, w := range wv.Weights {
		if w.Confidence >= minConfidence {
			filtered = append(filtered, w)
		}
	}
	wv.Weights = filtered
}

// TotalExposure returns the sum of absolute weights (leverage indicator)
func (wv *WeightVector) TotalExposure() float64 {
	var sum float64
	for _, w := range wv.Weights {
		sum += abs64(w.Weight)
	}
	return sum
}

// NetExposure returns the net directional exposure (long - short)
func (wv *WeightVector) NetExposure() float64 {
	var sum float64
	for _, w := range wv.Weights {
		sum += w.Weight
	}
	return sum
}

// LongCount returns the number of long positions
func (wv *WeightVector) LongCount() int {
	count := 0
	for _, w := range wv.Weights {
		if w.Weight > 0 {
			count++
		}
	}
	return count
}

// ShortCount returns the number of short positions
func (wv *WeightVector) ShortCount() int {
	count := 0
	for _, w := range wv.Weights {
		if w.Weight < 0 {
			count++
		}
	}
	return count
}

// --- Pipeline Stage Interface ---

// PipelineStage is implemented by each module in the weight-centric pipeline
type PipelineStage interface {
	// Name returns the stage identifier
	Name() string

	// Process transforms the weight vector through this stage
	Process(wv *WeightVector) (*WeightVector, error)
}

// PipelineChain runs weight vectors through multiple stages in order
type PipelineChain struct {
	stages []PipelineStage
}

// NewPipelineChain creates a pipeline with the given stages
func NewPipelineChain(stages ...PipelineStage) *PipelineChain {
	return &PipelineChain{stages: stages}
}

// Process runs the weight vector through all stages sequentially
func (pc *PipelineChain) Process(wv *WeightVector) (*WeightVector, error) {
	var err error
	for _, stage := range pc.stages {
		wv, err = stage.Process(wv)
		if err != nil {
			return nil, err
		}
	}
	return wv, nil
}

// Stages returns the list of stage names
func (pc *PipelineChain) Stages() []string {
	names := make([]string, len(pc.stages))
	for i, s := range pc.stages {
		names[i] = s.Name()
	}
	return names
}

func abs64(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
