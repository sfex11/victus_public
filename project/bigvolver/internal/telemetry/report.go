package telemetry

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"
)

// DailyReportScheduler generates and sends daily performance reports
type DailyReportScheduler struct {
	mlflowClient *MLflowClient
	reportHour   int // KST hour (default 8)
	chatID       string
	botToken     string
}

// DailyReportData holds the data for a daily report
type DailyReportData struct {
	Date        string  `json:"date"`
	MLVersion   string  `json:"ml_version"`
	MLSharpe    float64 `json:"ml_sharpe"`
	MLWinRate   float64 `json:"ml_win_rate"`
	PPOVersion  string  `json:"ppo_version"`
	PPOSharpe   float64 `json:"ppo_sharpe"`
	SACVersion  string  `json:"sac_version"`
	SACSharpe   float64 `json:"sac_sharpe"`
	EnsembleML  float64 `json:"ensemble_ml_pct"`
	EnsembleDRL float64 `json:"ensemble_drl_pct"`
	BestModel   string  `json:"best_model"`
	BestSharpe  float64 `json:"best_sharpe"`
	MLflowOK    bool    `json:"mlflow_ok"`
}

// NewDailyReportScheduler creates a new scheduler
func NewDailyReportScheduler(mlflowURL string, opts ...ReportOption) *DailyReportScheduler {
	s := &DailyReportScheduler{
		mlflowClient: NewMLflowClient(mlflowURL),
		reportHour:   8, // 08:00 KST
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// ReportOption configures the report scheduler
type ReportOption func(*DailyReportScheduler)

// WithReportHour sets the report hour (KST)
func WithReportHour(h int) ReportOption {
	return func(s *DailyReportScheduler) { s.reportHour = h }
}

// WithTelegram sets Telegram credentials
func WithTelegram(botToken, chatID string) ReportOption {
	return func(s *DailyReportScheduler) {
		s.botToken = botToken
		s.chatID = chatID
	}
}

// StartDaily runs the scheduler — checks every minute, sends at reportHour KST
func (s *DailyReportScheduler) StartDaily(ctx context.Context) {
	log.Printf("[DailyReport] Scheduler started. Report at %02d:00 KST daily.", s.reportHour)

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	lastSent := ""

	for {
		select {
		case <-ctx.Done():
			log.Println("[DailyReport] Scheduler stopped.")
			return
		case <-ticker.C:
			now := time.Now()
			kst := now.UTC().Add(9 * time.Hour)

			today := kst.Format("2006-01-02")

			// Send once per day at reportHour
			if kst.Hour() == s.reportHour && kst.Minute() == 0 && today != lastSent {
				data, err := s.CollectReportData()
				if err != nil {
					log.Printf("[DailyReport] Failed to collect data: %v", err)
					continue
				}

				report := s.FormatReport(data)

				if s.botToken != "" && s.chatID != "" {
					if err := s.sendTelegram(report); err != nil {
						log.Printf("[DailyReport] Telegram send failed: %v", err)
					} else {
						log.Printf("[DailyReport] Report sent to Telegram.")
					}
				} else {
					log.Printf("[DailyReport] Report (no Telegram configured):\n%s", report)
				}

				lastSent = today
			}
		}
	}
}

// CollectReportData gathers metrics from MLflow for the daily report
func (s *DailyReportScheduler) CollectReportData() (*DailyReportData, error) {
	data := &DailyReportData{
		Date: time.Now().UTC().Add(9 * time.Hour).Format("2006-01-02"),
	}

	// Check MLflow connectivity
	err := s.mlflowClient.HealthCheck()
	data.MLflowOK = (err == nil)

	// Get ML metrics
	mlMetrics, err := s.mlflowClient.GetLatestRunMetrics("lightgbm")
	if err == nil {
		if v, ok := mlMetrics["sharpe_ratio"]; ok {
			data.MLSharpe = v
		}
		if v, ok := mlMetrics["win_rate"]; ok {
			data.MLWinRate = v
		}
	}

	// Get PPO metrics
	ppoMetrics, err := s.mlflowClient.GetLatestRunMetrics("ppo")
	if err == nil {
		if v, ok := ppoMetrics["sharpe_ratio"]; ok {
			data.PPOSharpe = v
		}
	}

	// Get SAC metrics
	sacMetrics, err := s.mlflowClient.GetLatestRunMetrics("sac")
	if err == nil {
		if v, ok := sacMetrics["sharpe_ratio"]; ok {
			data.SACSharpe = v
		}
	}

	// Calculate ensemble weights (Sharpe²)
	mlW := math.Pow(data.MLSharpe, 2)
	drlSharpe := math.Max(data.PPOSharpe, data.SACSharpe)
	drlW := math.Pow(drlSharpe, 2)
	total := mlW + drlW
	if total > 0 {
		data.EnsembleML = mlW / total
		data.EnsembleDRL = drlW / total
	} else {
		data.EnsembleML = 0.5
		data.EnsembleDRL = 0.5
	}

	// Best model
	models := map[string]float64{
		"ML": data.MLSharpe,
		"PPO": data.PPOSharpe,
		"SAC": data.SACSharpe,
	}
	for name, sharpe := range models {
		if sharpe > data.BestSharpe {
			data.BestSharpe = sharpe
			data.BestModel = name
		}
	}

	return data, nil
}

// FormatReport formats report data as a Telegram-ready message
func (s *DailyReportScheduler) FormatReport(data *DailyReportData) string {
	weekday := func(t time.Time) string {
		days := []string{"월", "화", "수", "목", "금", "토", "일"}
		return days[t.Weekday()]
	}

	now := time.Now().UTC().Add(9 * time.Hour)
	dateStr := now.Format("2006-01-02")

	mlflowStatus := "✅ 연결됨"
	if !data.MLflowOK {
		mlflowStatus = "❌ 미연결"
	}

	return fmt.Sprintf(
		"📊 <b>BigVolver 일일 성과 리포트</b>\n"+
			"📅 %s (%s)\n\n"+
			"<b>🔧 모델 현황</b>\n"+
			"  ML (LightGBM): Sharpe=%.2f | WR=%.1f%%\n"+
			"  DRL (PPO): Sharpe=%.2f\n"+
			"  DRL (SAC): Sharpe=%.2f\n\n"+
			"<b>⚖️ 앙상블 비중</b>\n"+
			"  ML: %.0f%% | DRL: %.0f%%\n"+
			"  최고 성능: <b>%s</b> (Sharpe: %.2f)\n\n"+
			"<b>📡 시스템 상태</b>\n"+
			"  MLflow: %s",
		dateStr, weekday(now),
		data.MLSharpe, data.MLWinRate*100,
		data.PPOSharpe, data.SACSharpe,
		data.EnsembleML*100, data.EnsembleDRL*100,
		data.BestModel, data.BestSharpe,
		mlflowStatus,
	)
}

// sendTelegram sends a message via Telegram bot API
func (s *DailyReportScheduler) sendTelegram(text string) error {
	if s.botToken == "" || s.chatID == "" {
		return fmt.Errorf("telegram not configured")
	}

	// Use the notify package if available, otherwise direct HTTP call
	url := fmt.Sprintf(
		"https://api.telegram.org/bot%s/sendMessage?chat_id=%s&text=%s&parse_mode=HTML",
		s.botToken, s.chatID, text,
	)

	client := &MLflowClient{} // Reuse HTTP client
	_, err := client.httpClient.Get(url)
	return err
}
