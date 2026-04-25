# Phase B — Self-Adaptive Retraining 명세서

**작성:** 헥스 (2026-04-11)
**대상:** 빅터스 (B1~B3), 헥스 (B4)

---

## 개요

6시간 주기로 모델을 자동 재훈련하고, 성능 저하 시 이전 버전으로 롤백하는 시스템.

---

## B1: RetrainScheduler (`internal/ml/retrainer.go`)

### 역할
정기적으로 재훈련을 트리거하는 스케줄러.

### 인터페이스
```go
type RetrainScheduler struct {
    predictor  *ml.Predictor
    notifier   *notify.TelegramNotifier
    interval   time.Duration     // 기본 6시간
    symbols    []string          // 학습 대상 종목
    windowDays int               // 학습 데이터 윈도우 (기본 30일)
    lastRetrain time.Time
}

// Start는 백그라운드 고루틴으로 스케줄러를 실행
func (s *RetrainScheduler) Start(ctx context.Context)

// RetrainNow는 수동 재훈련을 트리거
func (s *RetrainScheduler) RetrainNow(symbol string) error

// Status는 스케줄러 상태 반환
func (s *RetrainScheduler) Status() SchedulerStatus
```

### 재훈련 플로우
1. `ExportTrainingData()`로 JSONL 생성
2. Python 서비스 `/retrain` 호출 (records body 전송)
3. 응답에서 Sharpe, WinRate, ModelVersion 확인
4. `notifier.NotifyRetrainResult()`로 결과 Telegram 전송
5. 성능 지표를 `ModelRegistry`에 저장

---

## B2: DataWindow (`internal/ml/data_window.go`)

### 역할
학습 데이터 윈도우를 슬라이딩하여 최신 30일 데이터를 제공.

### 인터페이스
```go
type DataWindow struct {
    featurePipeline *ml.FeaturePipeline
    windowDays      int       // 기본 30일
    minSamples      int       // 기본 500
}

// GetTrainingWindow는 지정 종목의 최신 학습 데이터를 반환
func (dw *DataWindow) GetTrainingWindow(symbol string) ([]*ml.FeatureSet, error)

// GetTrainingRecords는 JSONL용 레코드 형태로 반환
func (dw *DataWindow) GetTrainingRecords(symbol string) ([]map[string]interface{}, error)
```

### 핵심 로직
- 최근 `windowDays`일의 캔들에서 피처셋 생성
- 샘플 수 < minSamples면 윈도우 확장 (최대 60일)
- `funding_rate_change_1h`, `funding_rate_change_8h`은
  `GetLatestCandles()`로 최근 200캔들을 가져와 계산

---

## B3: ModelRegistry + Auto Rollback

### ModelRegistry (`internal/ml/model_registry.go`)

### 역할
모델 버전 히스토리를 관리하고, 성능 저하 시 자동 롤백.

### 인터페이스
```go
type ModelVersion struct {
    Version      string
    TrainedAt    time.Time
    SharpeRatio  float64
    WinRate      float64
    SamplesUsed  int
    Active       bool
}

type ModelRegistry struct {
    versions    []ModelVersion
    current     *ModelVersion
    maxHistory  int             // 보관할 최대 버전 수 (기본 10)
    rollbackThreshold float64   // Sharpe 하락 임계값 (기본 30%)
}

// RecordVersion은 새 모델 버전을 기록
func (r *ModelRegistry) RecordVersion(v ModelVersion) error

// ShouldRollback은 새 모델이 이전보다 성능이 낮은지 판단
func (r *ModelRegistry) ShouldRollback(newVersion ModelVersion) (bool, string)

// ActivateVersion은 지정 버전을 활성화
func (r *ModelRegistry) ActivateVersion(version string) error

// GetCurrent는 현재 활성 모델 정보 반환
func (r *ModelRegistry) GetCurrent() *ModelVersion

// GetHistory는 버전 히스토리 반환
func (r *ModelRegistry) GetHistory() []ModelVersion
```

### 롤백 조건
1. 새 모델의 Sharpe가 이전 모델 대비 30% 이상 하락
2. 새 모델의 Sharpe가 0 미만
3. Walk-forward CV에서 3개 이상 fold가 음수 수익

롤백 시:
- Python 서비스에 이전 버전 로드 요청 (`/model/load?version=xxx`)
- `notifier.NotifyModelRollback()`으로 알림
- 빅터스가 `/model/load` 엔드포인트를 Python 서비스에 추가 필요

---

## B4: Telegram 알림 (헥스 완료 ✅)

### 산출물
- `internal/notify/telegram.go` — TelegramNotifier 구현

### 제공 메서드
| 메서드 | 용도 |
|--------|------|
| `SendMessage(text)` | 일반 메시지 전송 |
| `NotifyRetrainResult()` | 재훈련 결과 (Sharpe, WinRate) |
| `NotifyPerformanceAlert()` | 성능 저하 경고 |
| `NotifyModelRollback()` | 롤백 알림 |
| `NotifyDailyReport()` | 일일 성과 리포트 |

### 설정
```go
notifier := notify.NewTelegramNotifier(botToken, chatID)
```
- botToken, chatID는 환경변수 또는 config에서 주입
- `HEX_TELEGRAM_BOT_TOKEN`, `BIGVOLVER_CHAT_ID`

---

## Phase B 완료 조건

1. RetrainScheduler가 6시간마다 자동 재훈련
2. DataWindow가 최신 30일 데이터로 학습 데이터 생성
3. ModelRegistry가 버전 관리 + 자동 롤백
4. 모든 이벤트가 Telegram으로 알림
5. Python 서비스에 `/model/load` 엔드포인트 추가

---

## 빅터스에게 전달

B1~B3 구현 시 위 인터페이스를 따라주세요. B4(telegram.go)는 이미 완료했습니다.

Python 서비스에 `/model/load` 엔드포인트를 추가해 주세요:
```python
@app.route("/model/load", methods=["POST"])
def load_specific_version():
    version = request.json.get("version")
    # 모델 디렉토리에서 해당 버전 로드
```
