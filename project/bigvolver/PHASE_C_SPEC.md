# Phase C — Weight-Centric Pipeline 명세서

**작성:** 헥스 (2026-04-11)
**대상:** 빅터스 (C2~C4), 헥스 (C1)

---

## 개요

ML 예측을 최종 포트폴리오 비중(Weight)으로 변환하는 파이프라인.
각 모듈이 독립적인 `PipelineStage`로 동작하며, 체이닝으로 연결.

### 전체 흐름
```
Market Data → ML Selector (C2) → Timing (C4) → Risk Overlay (C3) → 최종 Weight Vector
```

---

## C1: Weight 인터페이스 (헥스 완료 ✅)

### 산출물
- `internal/pipeline/weight.go`

### 제공 타입
| 타입 | 설명 |
|------|------|
| `Weight` | 심볼별 비중 (-1.0~1.0), 신뢰도, 신호, 출처 |
| `WeightVector` | 전체 포트폴리오 비중 벡터 |
| `PipelineStage` | 각 파이프라인 모듈의 인터페이스 |
| `PipelineChain` | 다단계 파이프라인 실행기 |

### WeightVector 메서드
- `AddWeight(w)` — 비중 추가
- `GetWeight(symbol)` — 심볼별 조회
- `NormalizeWeights()` — 절대값 합 = 1.0으로 정규화
- `FilterByThreshold(conf)` — 최소 신뢰도 필터링
- `TotalExposure()` — 총 레버리지 (abs 합)
- `NetExposure()` — 순 방향성 노출 (long - short)
- `LongCount()`, `ShortCount()` — 포지션 수

### PipelineStage 인터페이스
```go
type PipelineStage interface {
    Name() string
    Process(wv *WeightVector) (*WeightVector, error)
}
```

### 사용 예
```go
chain := pipeline.NewPipelineChain(selector, timing, riskOverlay)
result, err := chain.Process(weightVector)
```

---

## C2: ML Selector (`internal/pipeline/selector.go`)

### 역할
ML 모델의 예측 결과를 Weight로 변환. 종목별 점수를 계산하고,
상위 N개 종목을 선정.

### 인터페이스
```go
type MLSelector struct {
    predictor *ml.Predictor
    dataWindow *ml.DataWindow
    pipeline  *ml.FeaturePipeline
    maxPositions int    // 최대 포지션 수 (기본 5)
    minConfidence float64 // 최소 신뢰도 (기본 0.3)
}

func NewMLSelector(predictor *ml.Predictor, dw *ml.DataWindow, fp *ml.FeaturePipeline, opts ...SelectorOption) *MLSelector

// SelectSymbols는 각 심볼에 대해 ML 예측 → Weight 생성
func (s *MLSelector) SelectSymbols(symbols []string) (*WeightVector, error)

// PipelineStage implementation
func (s *MLSelector) Name() string { return "ml_selector" }
func (s *MLSelector) Process(wv *WeightVector) (*WeightVector, error)
```

### 핵심 로직
1. 각 심볼에 대해 `FeaturePipeline.ComputeLatestFeatures()` 호출
2. `Predictor.Predict()`로 예측 → signal, confidence 획득
3. confidence < minConfidence 제외
4. confidence 기준으로 내림차순 정렬 후 상위 maxPositions 선정
5. confidence를 weight로 변환: `weight = confidence * signal_direction`
   - LONG → 양수, SHORT → 음수, NEUTRAL → 0

---

## C3: Risk Overlay (`internal/pipeline/risk_overlay.go`)

### 역할
최종 비중에 리스크 제약을 적용. 포트폴리오 수준의 리스크를 제어.

### 인터페이스
```go
type RiskConfig struct {
    MaxPositionSize    float64 // 단일 종목 최대 비중 (기본 0.3 = 30%)
    MaxTotalExposure   float64 // 총 노출 한도 (기본 1.0)
    MaxNetExposure     float64 // 순 방향 한도 (기본 0.5)
    MaxCorrelation     float64 // 종목 간 최대 상관관계 (기본 0.8)
    StopLossPct        float64 // 개별 종목 스탑로스 (%)
    MaxDrawdownPct     float64 // 포트폴리오 최대 DD 한도 (%)
    HighVolMultiplier  float64 // 고변동 시 비중 축소 배수 (기본 0.5)
}

type RiskOverlay struct {
    config     RiskConfig
    equityCurve []float64  // 과거 equity 추적
    mu         sync.RWMutex
}

func NewRiskOverlay(opts ...RiskOption) *RiskOverlay

func (r *RiskOverlay) Name() string { return "risk_overlay" }
func (r *RiskOverlay) Process(wv *WeightVector) (*WeightVector, error)
```

### 리스크 규칙 적용 순서
1. **포지션 크기 제한**: 단일 종목 weight ≤ MaxPositionSize
2. **총 노출 제한**: TotalExposure ≤ MaxTotalExposure (비례 축소)
3. **순 방향 제한**: |NetExposure| ≤ MaxNetExposure
4. **고변동 축소**: ATR이 평균의 2배 이상이면 HighVolMultiplier 곱함
5. **최대 DD 체크**: 현재 DD > MaxDrawdownPct면 전체 비중 반으로 축소
6. **재정규화**: 리스크 조정 후 NormalizeWeights()

---

## C4: Timing Module (`internal/pipeline/timing.go`)

### 역할
KAMA (Kaufman's Adaptive Moving Average) 기반으로 진입/퇴출 타이밍 조정.
가격이 KAMA 위/아래에 있는지로 비중을 조절.

### 인터페이스
```go
type TimingConfig struct {
    KAMAPeriod       int     // KAMA 기간 (기본 10)
    KAMAFastSC       float64 // 빠른 SC (기본 2/(2+1) = 0.6667)
    KAMASlowSC       float64 // 느린 SC (기본 2/(30+1) = 0.0645)
    EntryBufferPct   float64 // 진입 버퍼 (%) (기본 0.1)
    ExitBufferPct    float64 // 퇴출 버퍼 (%) (기본 0.2)
}

type TimingModule struct {
    config TimingConfig
    candleRepo *data.MarketDataRepository
}

func NewTimingModule(repo *data.MarketDataRepository, opts ...TimingOption) *TimingModule

func (t *TimingModule) Name() string { return "timing" }
func (t *TimingModule) Process(wv *WeightVector) (*WeightVector, error)
```

### KAMA 계산
```
ER = |direction| / volatility
  direction = close - close[period]
  volatility = sum(|close[i] - close[i-1]|) for i in [period]

SC = [ER * (fastSC - slowSC) + slowSC]^2
KAMA = KAMA[prev] + SC * (close - KAMA[prev])
```

### 타이밍 로직
- 가격 > KAMA + buffer → LONG 가중치 유지/증가
- 가격 < KAMA - buffer → SHORT 가중치 유지/증가
- KAMA ± buffer 사이 → NEUTRAL (비중 0.5x로 축소)
- 트렌드 전환 시 빠른 퇴출 (weight → 0)

---

## Phase C 완료 조건

1. Weight 인터페이스 정의 완료 ✅
2. MLSelector가 예측 → Weight 변환
3. TimingModule이 KAMA 기반 진입/퇴출 조정
4. RiskOverlay가 리스크 제약 적용
5. PipelineChain으로 3단계 연결 동작

---

## 빅터스에게 전달

C2~C4 구현 시:
- `PipelineStage` 인터페이스를 구현하세요 (`Name()` + `Process()`)
- `PipelineChain`으로 연결하면 자동으로 순차 실행됩니다
- 각 모듈의 `Process()`는 `WeightVector`를 받아 수정 후 반환
- 에러 발생 시 파이프라인 중단 → 원래 비중 유지 (fail-safe)
