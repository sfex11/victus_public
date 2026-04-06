package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/cors"

	"dsl-strategy-evolver/internal/ai"
	"dsl-strategy-evolver/internal/dsl"
	"dsl-strategy-evolver/internal/engine"
	"dsl-strategy-evolver/internal/evolver"
	"dsl-strategy-evolver/internal/rank"
)

// Handler handles HTTP requests
type Handler struct {
	evolver  *evolver.Evolver
	engine   *engine.Engine
	ranker   *rank.Ranker
	aiGen    *ai.Generator
}

// NewHandler creates a new handler
func NewHandler(evolver *evolver.Evolver, engine *engine.Engine, ranker *rank.Ranker, aiGen *ai.Generator) *Handler {
	return &Handler{
		evolver: evolver,
		engine:  engine,
		ranker:  ranker,
		aiGen:   aiGen,
	}
}

// RegisterRoutes registers all API routes
func (h *Handler) RegisterRoutes(router *gin.Engine) {
	api := router.Group("/api/evolver")
	{
		api.POST("/start", h.StartEvolver)
		api.POST("/stop", h.StopEvolver)
		api.GET("/status", h.GetStatus)
		api.GET("/strategies", h.ListStrategies)
		api.POST("/generate", h.GenerateStrategies)
		api.GET("/leaderboard", h.GetLeaderboard)
		api.POST("/search", h.WebSearch)
		api.GET("/regime", h.GetRegime)  // NEW: Regime detection
		api.GET("/regime/:symbol", h.GetRegimeBySymbol)  // NEW: Regime by symbol
	}
}

// WebSearch performs a web search using AI
// POST /api/evolver/search
func (h *Handler) WebSearch(c *gin.Context) {
	var req WebSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	if req.Query == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "query is required"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	result, err := h.aiGen.WebSearch(ctx, req.Query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, WebSearchResponse{
		Query:  req.Query,
		Result: result,
	})
}

// StartEvolver starts the evolution loop
// POST /api/evolver/start
func (h *Handler) StartEvolver(c *gin.Context) {
	var req StartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	// Update config if provided
	if req.EvolutionInterval > 0 {
		config := h.evolver.GetConfig()
		config.EvolutionInterval = time.Duration(req.EvolutionInterval) * time.Minute
		h.evolver.SetConfig(config)
	}

	if err := h.evolver.Start(); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, StartResponse{
		Status:        "running",
		StartedAt:     time.Now().Format(time.RFC3339),
		NextEvolution: time.Now().Add(h.evolver.GetConfig().EvolutionInterval).Format(time.RFC3339),
	})
}

// StopEvolver stops the evolution loop
// POST /api/evolver/stop
func (h *Handler) StopEvolver(c *gin.Context) {
	if err := h.evolver.Stop(); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	status := h.evolver.GetStatus()

	c.JSON(http.StatusOK, StopResponse{
		Status:         "stopped",
		StoppedAt:      time.Now().Format(time.RFC3339),
		CyclesCompleted: status.TotalCycles,
	})
}

// GetStatus returns the current status
// GET /api/evolver/status
func (h *Handler) GetStatus(c *gin.Context) {
	status := h.evolver.GetStatus()

	c.JSON(http.StatusOK, StatusResponse{
		Status:           getStatusString(status.Running),
		ActiveStrategies: status.ActiveStrategies,
		TotalGenerated:   status.TotalGenerated,
		TotalCycles:      status.TotalCycles,
		LastEvolution:    time.Now().Add(-h.evolver.GetConfig().EvolutionInterval).Format(time.RFC3339),
		NextEvolution:    status.NextEvolution.Format(time.RFC3339),
		BestStrategy:     status.BestStrategy,
		AverageMetrics:   status.AverageMetrics,
	})
}

// ListStrategies returns all strategies
// GET /api/evolver/strategies
func (h *Handler) ListStrategies(c *gin.Context) {
	limit := 100
	offset := 0
	sortBy := "score"
	order := "desc"

	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	if o := c.Query("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	if s := c.Query("sort"); s != "" {
		sortBy = s
	}

	if o := c.Query("order"); o != "" {
		order = o
	}

	// Get strategies
	strategies := h.engine.GetAllStrategies()

	// Convert to response format
	items := make([]StrategyItem, 0, len(strategies))
	for _, instance := range strategies {
		metrics := h.ranker.GetMetrics(instance.ID)
		score := 0.0
		if metrics != nil {
			score = h.ranker.CalculateScore(metrics)
		}

		items = append(items, StrategyItem{
			ID:        instance.ID,
			Name:      instance.Strategy.Name,
			Type:      string(instance.Strategy.Type),
			Symbol:    instance.Strategy.Symbol,
			State:     instance.State,
			CreatedAt: instance.Strategy.CreatedAt.Format(time.RFC3339),
			Metrics: StrategyMetrics{
				SharpeRatio:  getMetric(metrics, func(m *dsl.PerformanceMetrics) float64 { return m.SharpeRatio }),
				TotalReturn:  getMetric(metrics, func(m *dsl.PerformanceMetrics) float64 { return m.TotalReturn }),
				WinRate:      getMetric(metrics, func(m *dsl.PerformanceMetrics) float64 { return m.WinRate }),
				MaxDrawdown:  getMetric(metrics, func(m *dsl.PerformanceMetrics) float64 { return m.MaxDrawdown }),
			},
			Score: score,
		})
	}

	// Sort
	items = sortStrategies(items, sortBy, order)

	// Paginate
	total := len(items)
	if offset >= total {
		items = []StrategyItem{}
	} else {
		end := offset + limit
		if end > total {
			end = total
		}
		items = items[offset:end]
	}

	c.JSON(http.StatusOK, StrategiesResponse{
		Total:      total,
		Strategies: items,
	})
}

// GenerateStrategies generates new strategies
// POST /api/evolver/generate
func (h *Handler) GenerateStrategies(c *gin.Context) {
	var req GenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	if req.Count <= 0 {
		req.Count = 10
	}

	if req.Symbol == "" {
		req.Symbol = "BTCUSDT"
	}

	instances, err := h.evolver.GenerateStrategies(req.Count, req.Symbol)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	// Add to engine
	added := 0
	for _, instance := range instances {
		if err := h.engine.AddStrategy(instance); err == nil {
			h.ranker.RegisterStrategy(instance)
			added++
		}
	}

	// Convert to response
	strategies := make([]GeneratedStrategy, len(instances))
	for i, instance := range instances {
		metrics := h.ranker.GetMetrics(instance.ID)
		strategies[i] = GeneratedStrategy{
			ID:     instance.ID,
			Name:   instance.Strategy.Name,
			BacktestResult: &BacktestResultInfo{
				SharpeRatio:  getMetric(metrics, func(m *dsl.PerformanceMetrics) float64 { return m.SharpeRatio }),
				TotalReturn:  getMetric(metrics, func(m *dsl.PerformanceMetrics) float64 { return m.TotalReturn }),
			},
		}
	}

	c.JSON(http.StatusOK, GenerateResponse{
		Generated:        len(instances),
		BacktestPassed:   len(instances),
		AddedToTrading:   added,
		Strategies:       strategies,
	})
}

// GetLeaderboard returns the leaderboard
// GET /api/evolver/leaderboard
func (h *Handler) GetLeaderboard(c *gin.Context) {
	n := 10
	if nStr := c.Query("n"); nStr != "" {
		if parsed, err := strconv.Atoi(nStr); err == nil && parsed > 0 {
			n = parsed
		}
	}

	leaderboard := h.ranker.GetLeaderboard(n)

	c.JSON(http.StatusOK, LeaderboardResponse{
		Leaderboard: leaderboard,
	})
}

// Helper functions

func getStatusString(running bool) string {
	if running {
		return "running"
	}
	return "stopped"
}

func getMetric(metrics *dsl.PerformanceMetrics, getter func(*dsl.PerformanceMetrics) float64) float64 {
	if metrics == nil {
		return 0
	}
	return getter(metrics)
}

func sortStrategies(items []StrategyItem, sortBy, order string) []StrategyItem {
	// Simple implementation - in production, use proper sorting
	return items
}

// Request/Response types

type ErrorResponse struct {
	Error string `json:"error"`
}

type StartRequest struct {
	EvolutionInterval int `json:"evolution_interval"`
}

type StartResponse struct {
	Status        string `json:"status"`
	StartedAt     string `json:"started_at"`
	NextEvolution string `json:"next_evolution_at"`
}

type StopResponse struct {
	Status           string `json:"status"`
	StoppedAt        string `json:"stopped_at"`
	CyclesCompleted  int    `json:"cycles_completed"`
}

type StatusResponse struct {
	Status           string                       `json:"status"`
	ActiveStrategies int                          `json:"active_strategies"`
	TotalGenerated   int                          `json:"total_generated"`
	TotalCycles      int                          `json:"total_cycles"`
	LastEvolution    string                       `json:"last_evolution"`
	NextEvolution    string                       `json:"next_evolution"`
	BestStrategy     *evolver.StrategyInfo        `json:"best_strategy,omitempty"`
	AverageMetrics   *rank.AverageMetrics         `json:"average_metrics,omitempty"`
}

type StrategyMetrics struct {
	SharpeRatio  float64 `json:"sharpe_ratio"`
	TotalReturn  float64 `json:"total_return"`
	WinRate      float64 `json:"win_rate"`
	MaxDrawdown  float64 `json:"max_drawdown"`
}

type StrategyItem struct {
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	Type      string           `json:"type"`
	Symbol    string           `json:"symbol"`
	State     string           `json:"state"`
	CreatedAt string           `json:"created_at"`
	Metrics   StrategyMetrics  `json:"metrics"`
	Score     float64          `json:"score"`
}

type StrategiesResponse struct {
	Total      int             `json:"total"`
	Strategies []StrategyItem  `json:"strategies"`
}

type GenerateRequest struct {
	Count int    `json:"count"`
	Symbol string `json:"symbol"`
}

type BacktestResultInfo struct {
	SharpeRatio  float64 `json:"sharpe_ratio"`
	TotalReturn  float64 `json:"total_return"`
}

type GeneratedStrategy struct {
	ID              string              `json:"id"`
	Name            string              `json:"name"`
	BacktestResult  *BacktestResultInfo `json:"backtest_result"`
}

type GenerateResponse struct {
	Generated       int                 `json:"generated"`
	BacktestPassed  int                 `json:"backtest_passed"`
	AddedToTrading  int                 `json:"added_to_trading"`
	Strategies      []GeneratedStrategy `json:"strategies"`
}

type LeaderboardResponse struct {
	Leaderboard []*rank.LeaderboardEntry `json:"leaderboard"`
}

type WebSearchRequest struct {
	Query string `json:"query"`
}

type WebSearchResponse struct {
	Query  string `json:"query"`
	Result string `json:"result"`
}

// GetRegime returns the current market regime for default symbol
// GET /api/evolver/regime
func (h *Handler) GetRegime(c *gin.Context) {
	symbol := "BTCUSDT"
	h.getRegimeForSymbol(c, symbol)
}

// GetRegimeBySymbol returns the market regime for a specific symbol
// GET /api/evolver/regime/:symbol
func (h *Handler) GetRegimeBySymbol(c *gin.Context) {
	symbol := c.Param("symbol")
	if symbol == "" {
		symbol = "BTCUSDT"
	}
	h.getRegimeForSymbol(c, symbol)
}

func (h *Handler) getRegimeForSymbol(c *gin.Context, symbol string) {
	// Get market data from market repo
	marketRepo := h.engine.GetMarketRepo()
	candles, err := marketRepo.GetLatestCandles(symbol, 100)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	if len(candles) < 50 {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "insufficient data"})
		return
	}

	// Extract OHLC data
	highs := make([]float64, len(candles))
	lows := make([]float64, len(candles))
	closes := make([]float64, len(candles))
	for i, candle := range candles {
		highs[i] = candle.High
		lows[i] = candle.Low
		closes[i] = candle.Close
	}

	// Detect regime
	detector := engine.NewRegimeDetector()
	regimeInfo := detector.Detect(highs, lows, closes)

	c.JSON(http.StatusOK, RegimeResponse{
		Symbol:        symbol,
		Regime:        string(regimeInfo.Regime),
		EMAFast:       regimeInfo.EMAFast,
		EMASlow:       regimeInfo.EMASlow,
		ATR:           regimeInfo.ATR,
		ATRPercent:    regimeInfo.ATRPercent,
		ATRMean:       regimeInfo.ATRMean,
		RSI:           regimeInfo.RSI,
		TrendStrength: regimeInfo.TrendStrength,
		VolLevel:      regimeInfo.VolLevel,
		LastPrice:     closes[len(closes)-1],
		Timestamp:     time.Now().Format(time.RFC3339),
	})
}

type RegimeResponse struct {
	Symbol        string  `json:"symbol"`
	Regime        string  `json:"regime"`
	EMAFast       float64 `json:"ema_fast"`
	EMASlow       float64 `json:"ema_slow"`
	ATR           float64 `json:"atr"`
	ATRPercent    float64 `json:"atr_percent"`
	ATRMean       float64 `json:"atr_mean"`
	RSI           float64 `json:"rsi"`
	TrendStrength float64 `json:"trend_strength"`
	VolLevel      float64 `json:"vol_level"`
	LastPrice     float64 `json:"last_price"`
	Timestamp     string  `json:"timestamp"`
}

// StartServer starts the HTTP server
func StartServer(handler *Handler, port int) error {
	// Set gin mode
	gin.SetMode(gin.ReleaseMode)

	router := gin.Default()

	// Add CORS
	corsHandler := cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: true,
	})
	router.Use(func(c *gin.Context) {
		corsHandler.HandlerFunc(c.Writer, c.Request)
		c.Next()
	})

	// Register routes
	handler.RegisterRoutes(router)

	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	addr := fmt.Sprintf(":%d", port)
	fmt.Printf("[API] Starting server on %s\n", addr)
	return router.Run(addr)
}
