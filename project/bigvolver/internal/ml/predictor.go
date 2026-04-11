package ml

// Predictor bridges Go with the Python LightGBM service for predictions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// PredictionRequest is sent to the Python ML service
type PredictionRequest struct {
	Symbol   string             `json:"symbol"`
	Features map[string]float64 `json:"features"`
}

// PredictionResponse is received from the Python ML service
type PredictionResponse struct {
	PredictedReturn float64 `json:"predicted_return"`
	Signal          string  `json:"signal"` // "LONG", "SHORT", "NEUTRAL"
	Confidence      float64 `json:"confidence"`
	ModelVersion    string  `json:"model_version"`
	ShapValues      map[string]float64 `json:"shap_values,omitempty"`
}

// RetrainRequest triggers model retraining
type RetrainRequest struct {
	Symbol       string `json:"symbol"`
	WindowSize   int    `json:"window_size_days"` // default 30
	MinSamples   int    `json:"min_samples"`      // default 500
}

// RetrainResponse reports retraining results
type RetrainResponse struct {
	Success       bool    `json:"success"`
	ModelVersion  string  `json:"model_version"`
	SharpeRatio   float64 `json:"sharpe_ratio"`
	WinRate       float64 `json:"win_rate"`
	SamplesUsed   int     `json:"samples_used"`
	TrainTimeSec  float64 `json:"train_time_sec"`
	ErrorMessage  string  `json:"error,omitempty"`
}

// Predictor communicates with the Python LightGBM service
type Predictor struct {
	baseURL    string
	httpClient *http.Client
	modelVer   string
}

// NewPredictor creates a new predictor pointing to the Python ML service
func NewPredictor(serviceURL string) *Predictor {
	return &Predictor{
		baseURL: serviceURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		modelVer: "unknown",
	}
}

// Predict sends features to the Python service and returns a prediction
func (p *Predictor) Predict(symbol string, features map[string]float64) (*PredictionResponse, error) {
	reqBody := PredictionRequest{
		Symbol:   symbol,
		Features: features,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := p.httpClient.Post(
		p.baseURL+"/predict",
		"application/json",
		bytes.NewReader(jsonBody),
	)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("service error (status %d): %s", resp.StatusCode, string(body))
	}

	var result PredictionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	p.modelVer = result.ModelVersion
	return &result, nil
}

// TriggerRetrain asks the Python service to retrain the model
func (p *Predictor) TriggerRetrain(req RetrainRequest) (*RetrainResponse, error) {
	if req.WindowSize == 0 {
		req.WindowSize = 30
	}
	if req.MinSamples == 0 {
		req.MinSamples = 500
	}

	jsonBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal retrain request: %w", err)
	}

	resp, err := p.httpClient.Post(
		p.baseURL+"/retrain",
		"application/json",
		bytes.NewReader(jsonBody),
	)
	if err != nil {
		return nil, fmt.Errorf("retrain request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("retrain error (status %d): %s", resp.StatusCode, string(body))
	}

	var result RetrainResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode retrain response: %w", err)
	}

	return &result, nil
}

// HealthCheck verifies the Python ML service is alive
func (p *Predictor) HealthCheck() error {
	resp, err := p.httpClient.Get(p.baseURL + "/health")
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("service unhealthy (status %d)", resp.StatusCode)
	}
	return nil
}

// GetModelVersion returns the current model version
func (p *Predictor) GetModelVersion() string {
	return p.modelVer
}
