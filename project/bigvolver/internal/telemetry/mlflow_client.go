package telemetry

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// MLflowClient accesses MLflow REST API from Go
type MLflowClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewMLflowClient creates a new MLflow client
func NewMLflowClient(baseURL string) *MLflowClient {
	return &MLflowClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// MetricPoint represents a single metric data point
type MetricPoint struct {
	Timestamp int64   `json:"timestamp"`
	Value     float64 `json:"value"`
	Step      int     `json:"step"`
}

// RunInfo represents an MLflow run
type RunInfo struct {
	RunID      string            `json:"run_id"`
	Status     string            `json:"status"`
	StartTime  int64             `json:"start_time"`
	EndTime    int64             `json:"end_time"`
	ArtifactURI string           `json:"artifact_uri"`
	Lifecycle  string            `json:"lifecycle_stage"`
	Params     map[string]string `json:"params"`
	Metrics    map[string]float64 `json:"metrics"`
	Tags       map[string]string `json:"tags"`
}

// ExperimentInfo represents an MLflow experiment
type ExperimentInfo struct {
	ExperimentID   string `json:"experiment_id"`
	Name           string `json:"name"`
	ArtifactLocation string `json:"artifact_location"`
	LifecycleStage string `json:"lifecycle_stage"`
}

// HealthCheck verifies MLflow is reachable
func (c *MLflowClient) HealthCheck() error {
	resp, err := c.httpClient.Get(c.baseURL + "/api/2.0/mlflow/experiments/search")
	if err != nil {
		return fmt.Errorf("MLflow health check failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("MLflow unhealthy (status %d)", resp.StatusCode)
	}
	return nil
}

// GetExperiment finds an experiment by name
func (c *MLflowClient) GetExperiment(name string) (*ExperimentInfo, error) {
	body := fmt.Sprintf(`{"filter":"name='%s'"}`, name)

	resp, err := c.httpClient.Post(
		c.baseURL+"/api/2.0/mlflow/experiments/search",
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("search experiment: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var result struct {
		Experiments []ExperimentInfo `json:"experiments"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(result.Experiments) == 0 {
		return nil, fmt.Errorf("experiment '%s' not found", name)
	}

	return &result.Experiments[0], nil
}

// GetLatestRunMetrics gets the latest run's metrics for a given model type tag
func (c *MLflowClient) GetLatestRunMetrics(modelType string) (map[string]float64, error) {
	experiment, err := c.GetExperiment("bigvolver-v2")
	if err != nil {
		return nil, err
	}

	filter := fmt.Sprintf("tags.model_type = '%s' AND attributes.status = 'FINISHED'", modelType)
	body := fmt.Sprintf(`{
		"experiment_ids": ["%s"],
		"filter": "%s",
		"max_results": 1,
		"order_by": ["start_time DESC"]
	}`, experiment.ExperimentID, filter)

	resp, err := c.httpClient.Post(
		c.baseURL+"/api/2.0/mlflow/runs/search",
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("search runs: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var result struct {
		Runs []struct {
			Info struct {
				RunID     string `json:"run_id"`
				StartTime int64  `json:"start_time_ms"`
			} `json:"info"`
			Data struct {
				Metrics []struct {
					Key   string  `json:"key"`
					Value float64 `json:"value"`
				} `json:"metrics"`
			} `json:"data"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(result.Runs) == 0 {
		return nil, fmt.Errorf("no runs found for model_type=%s", modelType)
	}

	metrics := make(map[string]float64)
	for _, m := range result.Runs[0].Data.Metrics {
		metrics[m.Key] = m.Value
	}

	return metrics, nil
}

// GetMetricHistory retrieves the time series for a specific metric in a run
func (c *MLflowClient) GetMetricHistory(runID, metricKey string) ([]MetricPoint, error) {
	url := fmt.Sprintf(
		"%s/api/2.0/mlflow/metrics/get-history?run_id=%s&metric_key=%s",
		c.baseURL, runID, metricKey,
	)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("get metric history: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var result struct {
		Metrics []struct {
			Key   string `json:"key"`
			Values []struct {
				Timestamp int64   `json:"timestamp"`
				Value     float64 `json:"value"`
				Step      int     `json:"step"`
			} `json:"values"`
		} `json:"metrics"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(result.Metrics) == 0 {
		return nil, nil
	}

	points := make([]MetricPoint, 0, len(result.Metrics[0].Values))
	for _, v := range result.Metrics[0].Values {
		points = append(points, MetricPoint{
			Timestamp: v.Timestamp,
			Value:     v.Value,
			Step:      v.Step,
		})
	}

	return points, nil
}

// LogMetric logs a metric to a specific run
func (c *MLflowClient) LogMetric(runID, key string, value float64, step int) error {
	body := fmt.Sprintf(`{
		"run_id": "%s",
		"key": "%s",
		"value": %s,
		"timestamp": %d,
		"step": %d
	}`, runID, key, strconv.FormatFloat(value, 'f', -1, 64), time.Now().UnixMilli(), step)

	resp, err := c.httpClient.Post(
		c.baseURL+"/api/2.0/mlflow/runs/log-metric",
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("log metric: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("log metric error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// CreateRun creates a new MLflow run and returns the run ID
func (c *MLflowClient) CreateRun(experimentID, runName string, tags map[string]string) (string, error) {
	tagsJSON, _ := json.Marshal(tags)
	body := fmt.Sprintf(`{
		"experiment_id": "%s",
		"run_name": "%s",
		"tags": %s,
		"start_time": %d
	}`, experimentID, runName, string(tagsJSON), time.Now().UnixMilli())

	resp, err := c.httpClient.Post(
		c.baseURL+"/api/2.0/mlflow/runs/create",
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		return "", fmt.Errorf("create run: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	var result struct {
		Run struct {
			Info struct {
				RunID string `json:"run_id"`
			} `json:"info"`
		} `json:"run"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	return result.Run.Info.RunID, nil
}

// SearchRuns searches for runs matching a filter
func (c *MLflowClient) SearchRuns(experimentID, filter string, maxResults int) ([]RunInfo, error) {
	if filter == "" {
		filter = "attributes.status != 'killed'"
	}

	body := fmt.Sprintf(`{
		"experiment_ids": ["%s"],
		"filter": "%s",
		"max_results": %d,
		"order_by": ["start_time DESC"]
	}`, experimentID, filter, maxResults)

	resp, err := c.httpClient.Post(
		c.baseURL+"/api/2.0/mlflow/runs/search",
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("search runs: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var result struct {
		Runs []struct {
			Info struct {
				RunID       string `json:"run_id"`
				Status      string `json:"status"`
				StartTime   int64  `json:"start_time_ms"`
				EndTime     int64  `json:"end_time_ms"`
				ArtifactURI string `json:"artifact_uri"`
				Lifecycle   string `json:"lifecycle_stage"`
			} `json:"info"`
			Data struct {
				Params []struct {
					Key   string `json:"key"`
					Value string `json:"value"`
				} `json:"params"`
				Metrics []struct {
					Key   string  `json:"key"`
					Value float64 `json:"value"`
				} `json:"metrics"`
				Tags []struct {
					Key   string `json:"key"`
					Value string `json:"value"`
				} `json:"tags"`
			} `json:"data"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	runs := make([]RunInfo, 0, len(result.Runs))
	for _, r := range result.Runs {
		params := make(map[string]string)
		for _, p := range r.Data.Params {
			params[p.Key] = p.Value
		}

		metrics := make(map[string]float64)
		for _, m := range r.Data.Metrics {
			metrics[m.Key] = m.Value
		}

		tags := make(map[string]string)
		for _, t := range r.Data.Tags {
			tags[t.Key] = t.Value
		}

		runs = append(runs, RunInfo{
			RunID:      r.Info.RunID,
			Status:     r.Info.Status,
			StartTime:  r.Info.StartTime,
			EndTime:    r.Info.EndTime,
			ArtifactURI: r.Info.ArtifactURI,
			Lifecycle:  r.Info.Lifecycle,
			Params:     params,
			Metrics:    metrics,
			Tags:       tags,
		})
	}

	return runs, nil
}
