package ml

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"dsl-strategy-evolver/internal/notify"
)

// SchedulerStatus reports the current state of the retrain scheduler
type SchedulerStatus struct {
	Running       bool      `json:"running"`
	LastRetrain   time.Time `json:"last_retrain"`
	NextRetrain   time.Time `json:"next_retrain"`
	TotalRetrains int       `json:"total_retrains"`
	Interval      string    `json:"interval"`
	Symbols       []string  `json:"symbols"`
}

// RetrainScheduler triggers periodic model retraining
type RetrainScheduler struct {
	predictor   *Predictor
	notifier    *notify.TelegramNotifier
	registry    *ModelRegistry
	dataWindow  *DataWindow
	interval    time.Duration
	symbols     []string
	windowDays  int
	lastRetrain time.Time
	totalCount  int
	mu          sync.RWMutex
}

// NewRetrainScheduler creates a new retrain scheduler
func NewRetrainScheduler(
	predictor *Predictor,
	notifier *notify.TelegramNotifier,
	registry *ModelRegistry,
	dataWindow *DataWindow,
	symbols []string,
	opts ...SchedulerOption,
) *RetrainScheduler {
	s := &RetrainScheduler{
		predictor:  predictor,
		notifier:   notifier,
		registry:   registry,
		dataWindow: dataWindow,
		interval:   6 * time.Hour,
		symbols:    symbols,
		windowDays: 30,
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// SchedulerOption configures the retrain scheduler
type SchedulerOption func(*RetrainScheduler)

// WithInterval sets the retrain interval
func WithInterval(d time.Duration) SchedulerOption {
	return func(s *RetrainScheduler) { s.interval = d }
}

// WithRetrainWindow sets the training data window size
func WithRetrainWindow(days int) SchedulerOption {
	return func(s *RetrainScheduler) { s.windowDays = days }
}

// Start runs the scheduler in a background goroutine
func (s *RetrainScheduler) Start(ctx context.Context) {
	s.mu.Lock()
	s.lastRetrain = time.Now() // Don't retrain immediately on start
	s.mu.Unlock()

	log.Printf("[RetrainScheduler] Started. Interval: %s, Symbols: %v", s.interval, s.symbols)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[RetrainScheduler] Stopped.")
			return
		case <-ticker.C:
			for _, symbol := range s.symbols {
				if err := s.RetrainNow(symbol); err != nil {
					log.Printf("[RetrainScheduler] Retrain failed for %s: %v", symbol, err)
				}
			}
		}
	}
}

// RetrainNow triggers an immediate retraining for a symbol
func (s *RetrainScheduler) RetrainNow(symbol string) error {
	log.Printf("[RetrainScheduler] Retraining %s...", symbol)

	// 1. Get training data from DataWindow
	records, err := s.dataWindow.GetTrainingRecords(symbol)
	if err != nil {
		err = fmt.Errorf("get training data: %w", err)
		s.notifier.NotifyRetrainResult(symbol, "", 0, 0, 0, err)
		return err
	}

	if len(records) < s.dataWindow.MinSamples() {
		err = fmt.Errorf("insufficient samples: %d (need >= %d)", len(records), s.dataWindow.MinSamples())
		s.notifier.NotifyRetrainResult(symbol, "", 0, 0, 0, err)
		return err
	}

	// 2. Call Python /retrain with records in body
	retrainReq := RetrainRequest{
		Symbol:     symbol,
		WindowSize: s.windowDays,
		MinSamples: s.dataWindow.MinSamples(),
	}

	reqBody := map[string]interface{}{
		"symbol":           retrainReq.Symbol,
		"window_size_days": retrainReq.WindowSize,
		"min_samples":      retrainReq.MinSamples,
		"records":          records,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal retrain request: %w", err)
	}

	retrainResp, err := s.predictor.TriggerRetrainWithRecords(jsonBody)
	if err != nil {
		s.notifier.NotifyRetrainResult(symbol, "", 0, 0, 0, err)
		return fmt.Errorf("retrain request: %w", err)
	}

	if !retrainResp.Success {
		err = fmt.Errorf("retrain failed: %s", retrainResp.ErrorMessage)
		s.notifier.NotifyRetrainResult(symbol, "", 0, 0, 0, err)
		return err
	}

	// 3. Check rollback conditions via ModelRegistry
	newVersion := ModelVersion{
		Version:     retrainResp.ModelVersion,
		TrainedAt:   time.Now(),
		SharpeRatio: retrainResp.SharpeRatio,
		WinRate:     retrainResp.WinRate,
		SamplesUsed: retrainResp.SamplesUsed,
		Active:      true,
	}

	shouldRollback, reason := s.registry.ShouldRollback(newVersion)
	if shouldRollback {
		// Rollback to previous version
		previous := s.registry.GetCurrent()
		if previous != nil {
			rollbackErr := s.predictor.LoadModelVersion(previous.Version)
			if rollbackErr != nil {
				log.Printf("[RetrainScheduler] Rollback load failed: %v", rollbackErr)
			}
			s.notifier.NotifyModelRollback(symbol, newVersion.Version, previous.Version, reason)
		}
		newVersion.Active = false
	}

	// 4. Record version in registry
	if err := s.registry.RecordVersion(newVersion); err != nil {
		log.Printf("[RetrainScheduler] Failed to record version: %v", err)
	}

	// 5. Notify result
	s.notifier.NotifyRetrainResult(
		symbol,
		retrainResp.ModelVersion,
		retrainResp.SharpeRatio,
		retrainResp.WinRate,
		retrainResp.SamplesUsed,
		nil,
	)

	// Update state
	s.mu.Lock()
	s.lastRetrain = time.Now()
	s.totalCount++
	s.mu.Unlock()

	log.Printf("[RetrainScheduler] %s retrained: v%s Sharpe=%.4f WR=%.1f%%",
		symbol, retrainResp.ModelVersion, retrainResp.SharpeRatio, retrainResp.WinRate*100)

	return nil
}

// Status returns the current scheduler status
func (s *RetrainScheduler) Status() SchedulerStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	next := s.lastRetrain.Add(s.interval)
	if next.Before(time.Now()) {
		next = time.Now() // Overdue
	}

	return SchedulerStatus{
		Running:       true,
		LastRetrain:   s.lastRetrain,
		NextRetrain:   next,
		TotalRetrains: s.totalCount,
		Interval:      s.interval.String(),
		Symbols:       s.symbols,
	}
}
