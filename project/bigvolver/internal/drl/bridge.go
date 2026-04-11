package drl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DRLWeight represents a single symbol's weight from DRL prediction
type DRLWeight struct {
	Symbol    string  `json:"symbol"`
	Weight    float64 `json:"weight"`
	Signal    string  `json:"signal"`
	Confidence float64 `json:"confidence"`
}

// DRLPrediction is the response from the DRL service
type DRLPrediction struct {
	Weights       []DRLWeight `json:"weights"`
	Algorithm     string      `json:"algorithm"`
	ModelVersion  string      `json:"model_version"`
	Confidence    float64     `json:"confidence"`
}

// DRLTrainRequest triggers DRL training
type DRLTrainRequest struct {
	Algorithm  string                   `json:"algorithm"`
	Symbol     string                   `json:"symbol"`
	Timesteps  int                      `json:"timesteps"`
	Data       []map[string]interface{} `json:"data,omitempty"`
}

// DRLTrainResponse reports training results
type DRLTrainResponse struct {
	Success       bool    `json:"success"`
	ModelVersion  string  `json:"model_version"`
	ModelPath     string  `json:"model_path"`
	MeanReward    float64 `json:"mean_reward"`
	SharpeRatio   float64 `json:"sharpe_ratio"`
	WinRate       float64 `json:"win_rate"`
	TotalReturn   float64 `json:"total_return"`
	MaxDrawdown   float64 `json:"max_drawdown"`
	TrainingTime  float64 `json:"training_time_sec"`
	Timesteps     int     `json:"timesteps"`
	DataRows      int     `json:"data_rows"`
	ErrorMessage  string  `json:"error,omitempty"`
}

// DRLBridge communicates with the Python DRL service
type DRLBridge struct {
	baseURL    string
	httpClient *http.Client
	modelVer   string
	algorithm  string
}

// NewDRLBridge creates a new DRL bridge
func NewDRLBridge(serviceURL string) *DRLBridge {
	return &DRLBridge{
		baseURL: serviceURL,
		httpClient: &http.Client{
			Timeout: 600 * time.Second, // DRL training can be long
		},
		algorithm: "ppo",
	}
}

// SetAlgorithm sets the default algorithm for predictions
func (b *DRLBridge) SetAlgorithm(algo string) {
	b.algorithm = algo
}

// Predict sends features and returns DRL-generated weights
func (b *DRLBridge) Predict(symbols map[string]map[string]float64, algorithm string) (*DRLPrediction, error) {
	if algorithm == "" {
		algorithm = b.algorithm
	}

	reqBody := map[string]interface{}{
		"features":  symbols,
		"algorithm": algorithm,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal predict request: %w", err)
	}

	resp, err := b.httpClient.Post(
		b.baseURL+"/drl/predict",
		"application/json",
		bytes.NewReader(jsonBody),
	)
	if err != nil {
		return nil, fmt.Errorf("predict request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("predict error (status %d): %s", resp.StatusCode, string(body))
	}

	var result DRLPrediction
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode predict response: %w", err)
	}

	b.modelVer = result.ModelVersion
	return &result, nil
}

// Train triggers DRL training
func (b *DRLBridge) Train(req DRLTrainRequest) (*DRLTrainResponse, error) {
	if req.Algorithm == "" {
		req.Algorithm = b.algorithm
	}
	if req.Timesteps == 0 {
		req.Timesteps = 100_000
	}
	if req.Symbol == "" {
		req.Symbol = "BTCUSDT"
	}

	jsonBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal train request: %w", err)
	}

	resp, err := b.httpClient.Post(
		b.baseURL+"/drl/train",
		"application/json",
		bytes.NewReader(jsonBody),
	)
	if err != nil {
		return nil, fmt.Errorf("train request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("train error (status %d): %s", resp.StatusCode, string(body))
	}

	var result DRLTrainResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode train response: %w", err)
	}

	if result.Success {
		b.modelVer = result.ModelVersion
	}

	return &result, nil
}

// HealthCheck verifies the DRL service is alive
func (b *DRLBridge) HealthCheck() error {
	resp, err := b.httpClient.Get(b.baseURL + "/drl/health")
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("service unhealthy (status %d)", resp.StatusCode)
	}
	return nil
}

// LoadModel loads a specific DRL model version
func (b *DRLBridge) LoadModel(version string) error {
	body := map[string]string{"version": version}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal load request: %w", err)
	}

	resp, err := b.httpClient.Post(
		b.baseURL+"/drl/model/load",
		"application/json",
		bytes.NewReader(jsonBody),
	)
	if err != nil {
		return fmt.Errorf("load model request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("load model error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// ListModels returns all available DRL models
func (b *DRLBridge) ListModels() ([]map[string]interface{}, error) {
	resp, err := b.httpClient.Get(b.baseURL + "/drl/model/list")
	if err != nil {
		return nil, fmt.Errorf("list models failed: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode list response: %w", err)
	}

	models, _ := result["models"].([]interface{})
	var modelList []map[string]interface{}
	for _, m := range models {
		if modelMap, ok := m.(map[string]interface{}); ok {
			modelList = append(modelList, modelMap)
		}
	}

	return modelList, nil
}

// GetModelVersion returns the current model version
func (b *DRLBridge) GetModelVersion() string {
	return b.modelVer
}
