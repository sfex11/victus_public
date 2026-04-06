# DESIGN - DSL 전략 탐색 시스템 (Strategy Evolver)

## 1. 아키텍처 개요

### 1.1 시스템 다이어그램

```
┌─────────────────────────────────────────────────────────────────────────┐
│                           HTTP API Layer (Port 3004)                    │
│  ┌──────────┬──────────┬──────────┬──────────┬──────────┬──────────┐   │
│  │ /start   │ /stop    │ /status  │ /generate│/leaderboard│ /strategies│ │
│  └──────────┴──────────┴──────────┴──────────┴──────────┴──────────┘   │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                        Evolver Orchestrator                             │
│  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐     │
│  │  Evolution Loop  │  │ Strategy Manager │  │  Rank Calculator │     │
│  │  Controller      │  │  (100 slots)     │  │  (Performance)   │     │
│  └──────────────────┘  └──────────────────┘  └──────────────────┘     │
└─────────────────────────────────────────────────────────────────────────┘
          │                    │                    │
          ▼                    ▼                    ▼
┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐
│   AI Generator   │  │  Paper Trading   │  │   Backtester     │
│  (OpenRouter)    │  │     Engine       │  │   (Historical)   │
│  5 Free Models   │  │  (Real-time)     │  │  1h Candles      │
└──────────────────┘  └──────────────────┘  └──────────────────┘
          │                    │                    │
          ▼                    ▼                    ▼
┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐
│   Model Pool     │  │  Position Mgr    │  │  Data Provider   │
│  Round-Robin     │  │  (Long+Short)    │  │  SQLite DB       │
└──────────────────┘  └──────────────────┘  └──────────────────┘
                                                     │
                            ┌────────────────────────┘
                            ▼
                  ┌─────────────────────┐
                  │  Shared Database    │
                  │  data/trading.db    │
                  │  - market_1h_candles │
                  │  - market_funding_rate│
                  └─────────────────────┘
```

### 1.2 컴포넌트 상호작용 흐름

```
진화 루프 (Evolution Loop):
┌──────────────────────────────────────────────────────────────────────┐
│ 1. GENERATE: AI Generator → N개 새 전략 생성                         │
│ 2. BACKTEST: Backtester → 과거 데이터로 1차 필터링                   │
│ 3. TRADE: Paper Trading Engine → 실시간 검증                          │
│ 4. EVALUATE: Rank Calculator → 성과 계산                              │
│ 5. SELECT: Strategy Manager → 하위 20% 제거, 상위 유지               │
│ 6. REPEAT: 주기마다 반복                                               │
└──────────────────────────────────────────────────────────────────────┘
```

---

## 2. 컴포넌트/모듈 상세 설계

### 2.1 DSL 파서 (`internal/dsl/`)

**책임**: YAML DSL을 실행 가능한 전략 객체로 변환

```go
// Parser
type Parser struct {
    validator *Validator
    exprEngine *ExpressionEngine
}

// Parse YAML to Strategy
func (p *Parser) Parse(yamlContent []byte) (*Strategy, error)

// Validate strategy rules
func (p *Parser) Validate(strategy *Strategy) error

// Expression evaluation context
type EvaluationContext struct {
    Price     float64
    Volume    float64
    Timestamp int64
    Indicators map[string]float64  // ema, rsi, etc.
    Positions  []Position
}
```

**DSL 문법 규칙**:
- 수학 연산자: `+`, `-`, `*`, `/`, `%`
- 비교 연산자: `<`, `>`, `<=`, `>=`, `==`, `!=`
- 논리 연산자: `&&`, `||`, `!`
- 내장 함수: `ema(period)`, `sma(period)`, `rsi(period)`, `max()`, `min()`
- 변수: `price`, `volume`, `funding_rate`

---

### 2.2 Paper Trading 엔진 (`internal/engine/`)

**책임**: 100개 전략 실시간 실행, 포지션 관리

```go
// Engine
type Engine struct {
    strategies      map[string]*StrategyInstance
    positionManager *PositionManager
    riskManager     *RiskManager
    eventBus        *EventBus
}

// StrategyInstance
type StrategyInstance struct {
    ID        string
    Strategy  *Strategy
    Positions map[string]*Position  // symbol -> Position
    Metrics   *PerformanceMetrics
    State     StrategyState
}

// Position (Hedge Mode: Long + Short)
type Position struct {
    ID          string
    StrategyID  string
    Symbol      string
    Side        Side  // LONG | SHORT
    Size        float64  // USDT
    EntryPrice  float64
    CurrentPrice float64
    UnrealizedPnL float64
    StopLoss    float64
    TakeProfit  float64
    EntryTime   int64
}

// Event-driven execution
func (e *Engine) OnMarketData(data MarketData)
func (e *Engine) EvaluateStrategies()
func (e *Engine) UpdatePositions()
func (e *Engine) CheckExitConditions()
```

**실행 사이클**:
```
Every 1 second:
1. Receive market data (WebSocket/REST polling)
2. Update strategy evaluation contexts
3. Check entry conditions for all strategies
4. Open positions if conditions met (within risk limits)
5. Check exit conditions for existing positions
6. Close positions if conditions met
7. Update performance metrics
```

---

### 2.3 AI Generator (`internal/ai/`)

**책임**: OpenRouter API로 새 전략 생성

```go
// Generator
type Generator struct {
    client     *openrouter.Client
    modelPool  *ModelPool
    promptTmpl *template.Template
}

// ModelPool - Round-robin rotation
type ModelPool struct {
    models    []string
    current   int
    mutex     sync.Mutex
}

func (mp *ModelPool) Next() string {
    mp.mutex.Lock()
    defer mp.mutex.Unlock()
    model := mp.models[mp.current]
    mp.current = (mp.current + 1) % len(mp.models)
    return model
}

// Generate N new strategies
func (g *Generator) GenerateStrategies(ctx context.Context, count int, topStrategies []*Strategy) ([]*Strategy, error)

// AI Prompt template
const promptTemplate = `
You are a quantitative trading strategy designer. Generate {{.Count}} unique trading strategies for crypto perpetual futures.

Symbol: {{.Symbol}}
Current top performing strategies (for inspiration):
{{range .TopStrategies}}
- {{.Name}}: Sharpe={{.Sharpe}}, WinRate={{.WinRate}}%
{{end}}

Requirements:
1. Use technical indicators: EMA, SMA, RSI, MACD
2. Define entry/exit conditions with expressions
3. Set stop-loss between 1-5%
4. Position size: 50-200 USDT
5. Hedge mode: Define both LONG and SHORT conditions

Output YAML format:
name: "StrategyName"
symbol: "BTCUSDT"
type: "hedge"
long:
  entry: "expression"
  exit: "expression"
  stop_loss: 0.02
short:
  entry: "expression"
  exit: "expression"
  stop_loss: 0.02
risk:
  position_size: 100
  max_positions: 2

Generate {{.Count}} different strategies, each with unique logic.`
```

---

### 2.4 Backtester (`internal/backtest/`)

**책임**: 과거 데이터로 전략 1차 필터링

```go
// Backtester
type Backtester struct {
    db         *sql.DB
    minTrades  int
    minSharpe  float64
}

// BacktestResult
type BacktestResult struct {
    StrategyID    string
    TotalReturn   float64
    SharpeRatio   float64
    MaxDrawdown   float64
    WinRate       float64
    TotalTrades   int
    ProfitFactor  float64
}

func (b *Backtester) Run(strategy *Strategy, startDate, endDate time.Time) (*BacktestResult, error)

// Filtering criteria
func (b *Backtester) IsAcceptable(result *BacktestResult) bool {
    return result.TotalTrades >= b.minTrades &&
           result.SharpeRatio >= b.minSharpe &&
           result.MaxDrawdown < 0.3  // 30% max drawdown
}
```

---

### 2.5 Evolver (`internal/evolver/`)

**책임**: 진화 루프 제어, 전략 수명주기 관리

```go
// Evolver
type Evolver struct {
    engine       *engine.Engine
    generator    *ai.Generator
    backtester   *backtest.Backtester
    ranker       *Ranker
    strategyRepo *StrategyRepository
    config       *Config
    stopCh       chan struct{}
}

// Evolution loop
func (e *Evolver) Start(ctx context.Context) error {
    ticker := time.NewTicker(e.config.EvolutionInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            e.RunEvolutionCycle()
        case <-e.stopCh:
            return nil
        case <-ctx.Done():
            return ctx.Err()
        }
    }
}

func (e *Evolver) RunEvolutionCycle() error {
    // 1. Get current leaderboard
    topStrategies := e.ranker.GetTopStrategies(10)

    // 2. Generate new strategies
    newStrategies, err := e.generator.GenerateStrategies(ctx, 20, topStrategies)

    // 3. Backtest all new strategies
    var passed []*Strategy
    for _, strategy := range newStrategies {
        result := e.backtester.Run(strategy, ...)
        if e.backtester.IsAcceptable(result) {
            passed = append(passed, strategy)
        }
    }

    // 4. Add to paper trading
    for _, strategy := range passed {
        e.engine.AddStrategy(strategy)
    }

    // 5. Replace bottom performers
    bottom := e.ranker.GetBottomStrategies(len(passed))
    for _, strategy := range bottom {
        e.engine.RemoveStrategy(strategy.ID)
    }

    return nil
}
```

---

### 2.6 Rank Calculator (`internal/rank/`)

**책임**: 성과 기반 순위 계산

```go
// Ranker
type Ranker struct {
    metrics map[string]*PerformanceMetrics
    mutex   sync.RWMutex
}

// PerformanceMetrics
type PerformanceMetrics struct {
    StrategyID      string
    TotalReturn     float64
    DailyReturn     float64
    WeeklyReturn    float64
    SharpeRatio     float64
    SortinoRatio    float64
    MaxDrawdown     float64
    WinRate         float64
    ProfitFactor    float64
    AvgTradeDuration time.Duration
    TotalTrades     int
    LastUpdated     time.Time
}

// Score calculation (weighted)
func (r *Ranker) CalculateScore(metrics *PerformanceMetrics) float64 {
    return metrics.SharpeRatio * 0.4 +
           metrics.WinRate * 0.3 +
           (1 - metrics.MaxDrawdown) * 0.2 +
           metrics.ProfitFactor * 0.1
}

func (r *Ranker) GetTopStrategies(n int) []*Strategy
func (r *Ranker) GetBottomStrategies(n int) []*Strategy
func (r *Ranker) UpdateMetrics(strategyID string, metrics *PerformanceMetrics)
```

---

### 2.7 API Handler (`internal/api/`)

**책임**: HTTP API 엔드포인트 구현

```go
// Handler
type Handler struct {
    evolver  *evolver.Evolver
    engine   *engine.Engine
    ranker   *rank.Ranker
}

// POST /api/evolver/start
func (h *Handler) StartEvolver(w http.ResponseWriter, r *http.Request)

// POST /api/evolver/stop
func (h *Handler) StopEvolver(w http.ResponseWriter, r *http.Request)

// GET /api/evolver/status
func (h *Handler) GetStatus(w http.ResponseWriter, r *http.Request)

// GET /api/evolver/strategies
func (h *Handler) ListStrategies(w http.ResponseWriter, r *http.Request)

// POST /api/evolver/generate
func (h *Handler) GenerateStrategies(w http.ResponseWriter, r *http.Request)

// GET /api/evolver/leaderboard
func (h *Handler) GetLeaderboard(w http.ResponseWriter, r *http.Request)
```

---

## 3. API 스펙 상세

### 3.1 POST /api/evolver/start
진화 루프 시작

**Request**:
```json
{
  "evolution_interval": "1h",
  "strategies_to_generate": 20,
  "replace_bottom_percent": 20
}
```

**Response**:
```json
{
  "status": "running",
  "started_at": "2026-03-21T10:00:00Z",
  "next_evolution_at": "2026-03-21T11:00:00Z"
}
```

---

### 3.2 POST /api/evolver/stop
진화 루프 중지

**Response**:
```json
{
  "status": "stopped",
  "stopped_at": "2026-03-21T10:30:00Z",
  "cycles_completed": 5
}
```

---

### 3.3 GET /api/evolver/status
현재 상태 조회

**Response**:
```json
{
  "status": "running",
  "active_strategies": 100,
  "total_generated": 245,
  "total_cycles": 12,
  "last_evolution": "2026-03-21T09:00:00Z",
  "next_evolution": "2026-03-21T10:00:00Z",
  "best_strategy": {
    "id": "strategy_042",
    "name": "MomentumRSI",
    "sharpe_ratio": 2.34,
    "total_return": 0.156
  }
}
```

---

### 3.4 GET /api/evolver/strategies
전체 전략 목록

**Query Parameters**:
- `limit`: 반환 개수 (default: 100)
- `offset`: 페이지 오프셋
- `sort`: 정렬 기준 (sharpe, return, winrate)
- `order`: asc/desc

**Response**:
```json
{
  "total": 100,
  "strategies": [
    {
      "id": "strategy_001",
      "name": "DualEMA",
      "type": "hedge",
      "symbol": "BTCUSDT",
      "created_at": "2026-03-20T15:30:00Z",
      "metrics": {
        "sharpe_ratio": 1.85,
        "total_return": 0.124,
        "win_rate": 0.58,
        "max_drawdown": 0.08
      }
    }
  ]
}
```

---

### 3.5 POST /api/evolver/generate
수동 전략 생성

**Request**:
```json
{
  "count": 10,
  "symbol": "BTCUSDT",
  "use_top_strategies_for_prompt": true
}
```

**Response**:
```json
{
  "generated": 10,
  "backtest_passed": 7,
  "added_to_trading": 7,
  "strategies": [
    {
      "id": "strategy_101",
      "name": "AI_Gen_V1",
      "backtest_result": {
        "sharpe_ratio": 1.92,
        "total_return": 0.145
      }
    }
  ]
}
```

---

### 3.6 GET /api/evolver/leaderboard
상위 전략 리더보드

**Query Parameters**:
- `n`: 반환 개수 (default: 10)

**Response**:
```json
{
  "leaderboard": [
    {
      "rank": 1,
      "id": "strategy_042",
      "name": "MomentumRSI",
      "score": 0.876,
      "metrics": {
        "sharpe_ratio": 2.34,
        "win_rate": 0.64,
        "total_return": 0.156,
        "max_drawdown": 0.06
      }
    }
  ]
}
```

---

## 4. 데이터 구조

### 4.1 데이터베이스 스키마

```sql
-- 전략 정의 저장
CREATE TABLE IF NOT EXISTS strategies (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    yaml_content TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    source TEXT,  -- 'ai_generated' | 'user_uploaded'
    parent_id TEXT,  -- AI 생성 시 기반이 된 전략
    generation INTEGER,  -- 진화 세대
    status TEXT,  -- 'active' | 'removed' | 'testing'
    FOREIGN KEY (parent_id) REFERENCES strategies(id)
);

-- Paper Trading 포지션
CREATE TABLE IF NOT EXISTS paper_positions (
    id TEXT PRIMARY KEY,
    strategy_id TEXT NOT NULL,
    symbol TEXT NOT NULL,
    side TEXT NOT NULL,  -- 'LONG' | 'SHORT'
    size_usdt REAL NOT NULL,
    entry_price REAL NOT NULL,
    exit_price REAL,
    entry_time INTEGER NOT NULL,
    exit_time INTEGER,
    stop_loss REAL,
    take_profit REAL,
    unrealized_pnl REAL,
    realized_pnl REAL,
    status TEXT NOT NULL,  -- 'OPEN' | 'CLOSED'
    FOREIGN KEY (strategy_id) REFERENCES strategies(id)
);

-- 성과 메트릭스 스냅샷
CREATE TABLE IF NOT EXISTS performance_metrics (
    id TEXT PRIMARY KEY,
    strategy_id TEXT NOT NULL,
    timestamp INTEGER NOT NULL,
    total_return REAL NOT NULL,
    sharpe_ratio REAL NOT NULL,
    win_rate REAL NOT NULL,
    max_drawdown REAL NOT NULL,
    profit_factor REAL NOT NULL,
    total_trades INTEGER NOT NULL,
    FOREIGN KEY (strategy_id) REFERENCES strategies(id)
);

-- 진화 루프 이력
CREATE TABLE IF NOT EXISTS evolution_history (
    id TEXT PRIMARY KEY,
    cycle_number INTEGER NOT NULL,
    timestamp INTEGER NOT NULL,
    strategies_generated INTEGER NOT NULL,
    strategies_added INTEGER NOT NULL,
    strategies_removed INTEGER NOT NULL,
    best_strategy_id TEXT,
    avg_sharpe_ratio REAL,
    UNIQUE(cycle_number)
);

-- AI 생성 로그
CREATE TABLE IF NOT EXISTS ai_generation_log (
    id TEXT PRIMARY KEY,
    timestamp INTEGER NOT NULL,
    model_used TEXT NOT NULL,
    prompt_hash TEXT NOT NULL,
    strategies_count INTEGER NOT NULL,
    success_count INTEGER NOT NULL,
    error_message TEXT
);
```

---

### 4.2 인메모리 구조

```go
// Strategy runtime instance
type StrategyInstance struct {
    ID              string
    Definition      *Strategy
    Positions       map[string]*Position
    LongCondition   *Expression
    ShortCondition  *Expression
    LongExit        *Expression
    ShortExit       *Expression
    Context         *EvaluationContext
    LastEvalTime    time.Time
}

// Evaluation context cache
type EvaluationContext struct {
    mu              sync.RWMutex
    Price           float64
    Volume          float64
    FundingRate     float64
    Indicators      map[string]*IndicatorSeries
    Candles         []*Candle  // Last 100 candles
}

// Indicator series for efficient calculation
type IndicatorSeries struct {
    Name     string
    Period   int
    Values   []float64
    LastUpdate time.Time
}

// Performance tracking
type PerformanceTracker struct {
    mu              sync.RWMutex
    Returns         []float64
    TradeHistory    []*Trade
    Drawdowns       []float64
    HighWatermark   float64
}
```

---

## 5. 팀 분할 계획

### 5.1 모듈별 책임 분할

| 모듈 | 주요 책임 | 예상 작업량 | 의존성 |
|------|-----------|------------|--------|
| **DSL Parser** | YAML 파싱, 표현식 평가, 검증 | 3일 | 없음 |
| **Paper Trading Engine** | 전략 실행, 포지션 관리, 리스크 제어 | 5일 | DSL Parser |
| **AI Generator** | OpenRouter 연동, 프롬프트 설계 | 2일 | 없음 |
| **Backtester** | 과거 데이터 검증, 메트릭 계산 | 3일 | DSL Parser |
| **Evolver** | 진화 루프 제어, 전략 수명주기 | 3일 | Engine, Generator, Backtester |
| **Rank Calculator** | 성과 계산, 순위 매기기 | 2일 | Engine |
| **API Handler** | HTTP 엔드포인트 구현 | 2일 | Evolver, Engine, Ranker |
| **Data Layer** | DB 연동, 캐싱 | 2일 | 모든 모듈 |

**총 예상 기간**: 22일 (약 3주)

---

### 5.2 병렬 작업 가능 조합

**Phase 1** (동시 진행 가능):
- DSL Parser + Data Layer

**Phase 2** (DSL 완료 후):
- Paper Trading Engine + Backtester (병렬)
- AI Generator (독립)

**Phase 3** (Engine/Backtest 완료 후):
- Rank Calculator
- Evolver
- API Handler

---

## 6. 체크리스트 (검증 가능)

### 6.1 DSL Parser
- [ ] YAML 파일을 파싱하여 Strategy 구조체로 변환
- [ ] 표현식 파싱: `price < ema(20) * 0.99`
- [ ] 내장 함수: `ema()`, `sma()`, `rsi()`, `max()`, `min()`
- [ ] 문법 오류 발생 시 명확한 에러 메시지
- [ ] 헤징 모드 타입 검증 (long/short 조건 존재)

### 6.2 Paper Trading Engine
- [ ] 100개 전략 동시 실행
- [ ] 롱+숏 동시 포지션 지원
- [ ] 1초마다 시장 데이터 평가
- [ ] 진입 조건 충족 시 자동 포지션 오픈
- [ ] 청산 조건 충족 시 자동 포지션 클로즈
- [ ] 손절/익절 가격 도달 시 즉시 청산
- [ ] 포지션별 미실현 손익 실시간 계산
- [ ] 전략별 리스크 한도 준수 (max_positions)

### 6.3 AI Generator
- [ ] OpenRouter API 연동
- [ ] 5개 모델 라운드로빈 로테이션
- [ ] AI 응답 파싱하여 YAML 전략 변환
- [ ] 프롬프트에 상위 전략 정보 포함
- [ ] API 실패 시 재시도 (최대 3회)
- [ ] 잘못된 YAML 응답 시 에러 처리

### 6.4 Backtester
- [ ] DB에서 과거 캔들 데이터 조회
- [ ] 전략 백테스팅 실행 (진입/청산 시뮬레이션)
- [ ] Sharpe Ratio 계산
- [ ] Max Drawdown 계산
- [ ] Win Rate 계산
- [ ] Profit Factor 계산
- [ ] 필터링 기준 적용 (최소 트레이드 수, 최소 Sharpe)

### 6.5 Evolver
- [ ] 진화 루프 시작/중지 제어
- [ ] 설정된 간격마다 사이클 실행
- [ ] AI로 새 전략 생성
- [ ] 백테스팅 필터링
- [ ] Paper Trading에 추가
- [ ] 하위 전략 제거 및 교체
- [ ] 진화 이력 DB 저장

### 6.6 Rank Calculator
- [ ] 실시간 성과 메트릭 계산
- [ ] 가중치 기반 종합 점수 산출
- [ ] 상위 N개 전략 조회
- [ ] 하위 N개 전략 조회
- [ ] 메트릭 스냅샷 DB 저장

### 6.7 API
- [ ] POST /api/evolver/start 정상 응답
- [ ] POST /api/evolver/stop 정상 응답
- [ ] GET /api/evolver/status 정상 응답
- [ ] GET /api/evolver/strategies 정상 응답
- [ ] POST /api/evolver/generate 정상 응답
- [ ] GET /api/evolver/leaderboard 정상 응답
- [ ] 에러 시 적절한 HTTP 상태 코드 반환

### 6.8 통합
- [ ] 기존 Go 백엔드 DB와 연동
- [ ] 시장 데이터 테이블 읽기
- [ ] 포트 3004에서 실행
- [ ] 100개 전략 60초 내 전부 평가
- [ ] 메모리 사용 500MB 이하

---

## 7. 테스트 시나리오

### 7.1 단위 테스트

#### DSL Parser
```go
func TestYAMLParsing(t *testing.T) {
    yaml := `
name: "TestStrategy"
symbol: "BTCUSDT"
type: "hedge"
long:
  entry: "price < 100"
  exit: "price > 110"
  stop_loss: 0.02
`
    strategy, err := parser.Parse([]byte(yaml))
    assert.NoError(t, err)
    assert.Equal(t, "TestStrategy", strategy.Name)
}
```

#### Expression Engine
```go
func TestExpressionEvaluation(t *testing.T) {
    ctx := &EvaluationContext{
        Price: 95000,
        Indicators: map[string]float64{"ema20": 94000},
    }
    result, err := exprEngine.Eval("price < ema(20) * 1.01", ctx)
    assert.NoError(t, err)
    assert.False(t, result) // 95000 < 94940 is false
}
```

#### Paper Trading Engine
```go
func TestPositionEntry(t *testing.T) {
    engine := NewEngine()
    strategy := createTestStrategy()
    engine.AddStrategy(strategy)

    // 시뮬레이션: 진입 조건 충족
    data := MarketData{Price: 90000, Symbol: "BTCUSDT"}
    engine.OnMarketData(data)

    positions := engine.GetPositions(strategy.ID)
    assert.Equal(t, 1, len(positions["LONG"]))
}
```

#### AI Generator
```go
func TestModelRotation(t *testing.T) {
    pool := NewModelPool([]string{
        "model1",
        "model2",
        "model3",
    })

    assert.Equal(t, "model1", pool.Next())
    assert.Equal(t, "model2", pool.Next())
    assert.Equal(t, "model3", pool.Next())
    assert.Equal(t, "model1", pool.Next()) // Rotate
}
```

---

### 7.2 통합 테스트

#### Evolution Cycle
```go
func TestEvolutionCycle(t *testing.T) {
    // 초기 전략 100개 로드
    evolver := setupTestEvolver(100)

    // 진화 사이클 실행
    err := evolver.RunEvolutionCycle()
    assert.NoError(t, err)

    // 20개 새 전략 생성
    // 백테스트 통과한 것만 추가
    // 하위 전략 제거됨
    status := evolver.GetStatus()
    assert.Equal(t, 100, status.ActiveStrategies)
}
```

#### Performance Under Load
```go
func Test100StrategiesPerformance(t *testing.T) {
    engine := NewEngine()

    // 100개 전략 추가
    for i := 0; i < 100; i++ {
        engine.AddStrategy(createRandomStrategy())
    }

    start := time.Now()
    engine.EvaluateAll()
    duration := time.Since(start)

    assert.Less(t, duration.Seconds(), float64(60)) // 60초 이내
}
```

#### Memory Usage
```go
func TestMemoryConstraints(t *testing.T) {
    engine := setupEngineWith100Strategies()

    var m runtime.MemStats
    runtime.ReadMemStats(&m)

    // 500MB 이하
    assert.Less(t, m.Alloc, uint64(500*1024*1024))
}
```

---

### 7.3 엔드투엔드 테스트

#### Full Evolution Workflow
```bash
# 1. 서버 시작
go run cmd/server/main.go

# 2. 진화 루프 시작
curl -X POST http://localhost:3004/api/evolver/start \
  -H "Content-Type: application/json" \
  -d '{"evolution_interval": "1h"}'

# 3. 상태 확인
curl http://localhost:3004/api/evolver/status

# 4. 리더보드 확인
curl http://localhost:3004/api/evolver/leaderboard?n=10

# 5. 수동 전략 생성
curl -X POST http://localhost:3004/api/evolver/generate \
  -H "Content-Type: application/json" \
  -d '{"count": 5}'

# 6. 진화 중지
curl -X POST http://localhost:3004/api/evolver/stop
```

---

### 7.4 성능 테스트

#### Concurrency Test
```go
func TestConcurrentStrategyUpdates(t *testing.T) {
    engine := NewEngine()
    addStrategies(engine, 100)

    // 동시에 시장 데이터 업데이트
    var wg sync.WaitGroup
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for j := 0; j < 100; j++ {
                data := generateRandomMarketData()
                engine.OnMarketData(data)
            }
        }()
    }
    wg.Wait()

    // 데드락 없이 완료되어야 함
}
```

---

## 8. 리스크 및 완화

### 8.1 기술적 리스크

| 리스크 | 영향 | 완화책 |
|--------|------|--------|
| OpenRouter API 장애 | 전략 생성 중단 | 재시도 로직, 여러 모델 사용 |
| DB 연결 실패 | 시스템 중단 | 재연결 로직, 로컬 캐시 |
| 메모리 누수 | 시스템 다운 | 주기적 모니터링, 프로파일링 |
| 표현식 평가 오류 | 전략 실행 실패 | 에러 핸들링, 검증 계층 |

### 8.2 트레이딩 리스크

| 리스크 | 영향 | 완화책 |
|--------|------|--------|
| 과적합 (Overfitting) | 실전 성과 저조 | Out-of-sample 백테스트 |
| 동시 전략 상관관계 | 집중 리스크 | 상관관계 모니터링 |
| 극단적 시장 조건 | 대폭락 | Max Drawdown 한도 |

---

## 9. 향후 확장

### 9.1 Phase 2 기능
- 다중 심볼 지원 (ETH, SOL 외 다른 코인)
- 앙상블 전략 (여러 전략 결합)
- 실전 트레이딩 연동 (Binance 실계좌)
- 웹 대시보드 (실시간 모니터링)

### 9.2 Phase 3 기능
- 강화학습 기반 전략 최적화
- 감정 분석 (뉴스, 소셜 미디어)
- 자동 리밸런싱
- 포트폴리오 관리

---

## 10. 참고 자료

- Binance Futures API: https://binance-docs.github.io/apidocs/futures/en/
- OpenRouter API: https://openrouter.ai/docs
- Go SQLite: https://github.com/mattn/go-sqlite3
- YAML 파싱: https://github.com/go-yaml/yaml
