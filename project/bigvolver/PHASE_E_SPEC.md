# Phase E — 실험 추적 & 모니터링 명세서

**작성:** 헥스 (2026-04-11)
**대상:** 빅터스 (E1), 헥스 (E2, E3)

---

## 개요

모델 학습/배포의 전체 생명주기를 추적하고,
일일 성과를 자동 리포팅하며,
전략 진화 히스토리를 시각화하는 대시보드를 제공.

### 아키텍처
```
Training Events ──→ Telemetry (W&B/MLflow) ──→ Metrics DB
                                                    │
Performance Data ──→ Daily Report Generator ──→ Telegram
                                                    │
Evolution History ──→ Dashboard Builder ──→ HTML Report
```

---

## E1: W&B / MLflow 통합 (빅터스)

**산출물:** `internal/telemetry/tracker.py` + `internal/telemetry/config.py`

### 역할
모든 학습 이벤트(ML 재훈련, DRL 학습)를 추적하고,
하이퍼파라미터, 메트릭, 아티팩트를 기록.

### 인터페이스
```python
class ExperimentTracker:
    """Unified experiment tracking for ML and DRL."""

    def __init__(
        self,
        backend: str = "mlflow",  # "mlflow" or "wandb"
        project_name: str = "bigvolver",
        tracking_uri: str = None,  # MLflow URI
    ):
        """
        backend:
          - "mlflow": 오픈소스, 자체 호스팅 가능, Go에서 REST API 접근
          - "wandb": 클라우드 기반, 훌륭한 시각화, 무료 티어
        """
        ...

    def log_training(
        self,
        model_type: str,         # "lightgbm" | "ppo" | "sac"
        version: str,
        params: dict,            # 하이퍼파라미터
        metrics: dict,           # sharpe, win_rate, mse, etc.
        artifacts: list = None,  # 모델 파일 경로들
        tags: dict = None,       # symbol, phase, data_range 등
    ) -> str:
        """
        학습 이벤트 로깅.
        Returns: run_id
        """
        ...

    def log_prediction(
        self,
        model_type: str,
        version: str,
        symbol: str,
        features: dict,
        prediction: dict,        # signal, confidence, predicted_return
    ) -> None:
        """실시간 예측 로깅 (샘플링 — 매 10번째 예측마다)."""
        ...

    def log_retrain(
        self,
        symbol: str,
        old_version: str,
        new_version: str,
        old_sharpe: float,
        new_sharpe: float,
        rolled_back: bool,
    ) -> None:
        """재훈련 이벤트 로깅 (롤백 포함)."""
        ...

    def get_latest_metrics(self, model_type: str) -> dict:
        """최근 학습 메트릭 조회."""
        ...

    def compare_runs(self, run_ids: list) -> pd.DataFrame:
        """여러 런의 메트릭 비교 테이블."""
        ...
```

### MLflow를 기본으로 선택한 이유
1. **자체 호스팅:** 회장 인프라에서 완전 독립 동작
2. **REST API:** Go에서 `localhost:5000/api/2.0/mlflow/runs/get-metric-history`로 직접 접근
3. **모델 레지스트리:** 내장 스테이징/프로덕션 전환
4. **의존성 최소:** `pip install mlflow`만 필요

### 로깅 포인트
| 이벤트 | 타이밍 | 로그 내용 |
|--------|--------|----------|
| ML 재훈련 | 6시간마다 | params, CV Sharpe, WinRate, MSE, samples |
| DRL 학습 | 수동/스케줄 | timesteps, reward, Sharpe, episode_stats |
| 예측 | 실시간 (샘플링) | symbol, features(요약), signal, confidence |
| 재훈련 롤백 | 롤백 시 | old_version, new_version, reason |
| 벤치마크 | 주간 | fold별 metrics, ensemble results |

### 기존 코드 통합 포인트
```python
# ml_service/server.py — retrain() 함수 끝에 추가:
tracker.log_training(
    model_type="lightgbm",
    version=model_version,
    params=params,
    metrics={"sharpe_ratio": cv_metrics["sharpe_ratio"], "win_rate": cv_metrics["win_rate"]},
    tags={"symbol": symbol, "phase": "retrain"},
)

# drl/api.py — train() 함수 끝에 추가:
tracker.log_training(
    model_type=algorithm,
    version=agent.model_version,
    params=train_info["hyperparams"],
    metrics={"sharpe_ratio": metrics["sharpe_ratio"], "mean_reward": train_info["eval_mean_reward"]},
    tags={"symbol": symbol, "phase": "drl_train"},
)
```

### Go에서 MLflow 접근
```go
// internal/telemetry/mlflow_client.go
type MLflowClient struct {
    baseURL string  // http://localhost:5000
}

// GetLatestRunMetrics gets the latest metrics for a model type
func (c *MLflowClient) GetLatestRunMetrics(modelType string) (map[string]float64, error)

// GetMetricHistory gets time series for a specific metric
func (c *MLflowClient) GetMetricHistory(runID, metricKey string) ([]MetricPoint, error)
```

---

## E2: 일일 성과 리포트 (헥스)

**산출물:** `internal/telemetry/daily_report.py` + `internal/telemetry/report.go`

### 역할
매일 정해진 시간에 전날의 포트폴리오 성과를 집계하여
Telegram으로 리포트를 전송.

### 리포트 포맷
```
📊 BigVolver 일일 성과 리포트
2026-04-11 (금)

┌─ 포트폴리오 ─────────────────────────┐
│ 초기 자본:  $10,000.00               │
│ 종료 자본:  $10,342.50               │
│ 일일 수익:  +3.43%                   │
│ 거래 횟수:  12                        │
├─ 리스크 ─────────────────────────────┤
│ Sharpe (30d):  1.82                  │
│ Max DD:       4.2%                   │
│ Net Exposure: 0.35                   │
├─ 모델 현황 ─────────────────────────┤
│ ML:  lgm-20260411-060000 (Sharpe 1.82)│
│ DRL: ppo-v20260411-020000 (Sharpe 1.45)│
│ 앙상블: ML 62% / DRL 38%             │
├─ 주요 거래 ─────────────────────────┤
│ 09:00 BTCUSDT LONG  +0.82%          │
│ 14:00 ETHUSDT SHORT +1.24%          │
│ 21:00 BTCUSDT EXIT  -0.31%          │
└─────────────────────────────────────┘
```

### Python 리포트 생성기
```python
# internal/telemetry/daily_report.py

class DailyReportGenerator:
    """Generates daily performance reports."""

    def __init__(
        self,
        mlflow_client: "ExperimentTracker",
        notifier: "TelegramNotifier",
        report_time: str = "08:00",  # KST, 매일 아침
    ):
        ...

    def generate_report(self, date: str = None) -> str:
        """
        일일 리포트 생성.

        데이터 소스:
        1. MLflow: 최근 24시간 예측/성과 메트릭
        2. ModelRegistry: 현재 모델 버전 + Sharpe
        3. RiskOverlay.equityCurve: 포트폴리오 equity
        4. 거래 로그: 당일 거래 내역
        """
        ...

    def send_report(self) -> None:
        """리포트를 Telegram으로 전송."""
        report = self.generate_report()
        self.notifier.SendMessage(report)

    def schedule_daily(self):
        """cron/scheduler로 매일 정시 실행."""
        ...
```

### Go 리포트 스케줄러
```go
// internal/telemetry/report.go

type DailyReportScheduler struct {
    mlflowClient *MLflowClient
    registry     *ml.ModelRegistry
    notifier     *notify.TelegramNotifier
    reportHour   int  // KST (기본 8시)
}

// StartDaily는 매일 reportHour에 리포트 전송
func (s *DailyReportScheduler) StartDaily(ctx context.Context)

// GenerateReport는 수동 리포트 생성
func (s *DailyReportScheduler) GenerateReport() (string, error)
```

---

## E3: 전략 진화 히스토리 대시보드 (헥스)

**산출물:** `internal/telemetry/dashboard.py` + `internal/telemetry/templates/dashboard.html`

### 역할
모든 Phase의 진행 상황과 모델 성능 변화를
단일 HTML 페이지로 시각화.

### 대시보드 섹션
```
┌─────────────────────────────────────────────────┐
│  BigVolver V2 — 전략 진화 대시보드                │
│  Last updated: 2026-04-11 17:00 KST              │
├──────────────┬──────────────────────────────────┤
│              │                                   │
│  Phase별     │  📈 Equity Curve                  │
│  진행 상황   │  [그래프]                          │
│              │                                   │
│  ✅ Phase A  │  📊 Sharpe Ratio History          │
│  ✅ Phase B  │  [그래프]                          │
│  ✅ Phase C  │                                   │
│  ✅ Phase D  │  🎯 모델 비교 (ML vs DRL)         │
│  🔄 Phase E  │  [테이블 + 레이더 차트]            │
│              │                                   │
├──────────────┤  🔄 재훈련 이력                    │
│  현재 모델   │  [타임라인]                        │
│  ML:  v...  │                                   │
│  DRL: v...  │  ⚠️ 알림 이력                      │
│              │  [리스트]                          │
└──────────────┴──────────────────────────────────┘
```

### 구현
```python
# internal/telemetry/dashboard.py

class DashboardBuilder:
    """Builds a self-contained HTML dashboard."""

    def __init__(
        self,
        mlflow_client: "ExperimentTracker",
        registry: "ModelRegistry",
        output_path: str = "./dashboard/index.html",
    ):
        ...

    def build(self) -> str:
        """
        대시보드 HTML 생성.

        데이터 소스:
        1. MLflow: 모든 run의 메트릭 히스토리
        2. ModelRegistry: 버전 히스토리 + 롤백 이력
        3. Phase 상태: config 또는 하드코딩
        4. 거래 로그: 최근 거래 내역

        기술:
        - Chart.js CDN으로 그래프 렌더링 (서버 사이드 불필요)
        - 단일 HTML 파일 (JS/CSS 인라인)
        - 자동 갱신: 5분마다 API 폴링 또는 정적 재생성
        """
        ...

    def get_model_comparison_data(self) -> dict:
        """ML vs DRL 비교 데이터."""
        ...

    def get_retrain_history(self) -> list:
        """재훈련/롤백 타임라인."""
        ...

    def get_phase_status(self) -> list:
        """Phase별 완료 상태."""
        ...
```

### 대시보드 자동 갱신
```python
# cron이나 Go 스케줄러에서 5분마다 실행
def refresh_dashboard():
    builder = DashboardBuilder(tracker, registry)
    builder.build()  # index.html 갱신
```

---

## Phase E 완료 조건

1. MLflow 서버 실행 중 (localhost:5000)
2. ML/DRL 학습 시 자동 메트릭 로깅
3. 매일 08:00 KST Telegram 일일 리포트 전송
4. HTML 대시보드에서 equity curve, Sharpe 히스토리, 모델 비교 확인 가능
5. Go에서 MLflow REST API로 메트릭 조회 가능

---

## 빅터스에게 전달

E1 구현 시:
- **MLflow 기본**으로 구현 (W&B는 향후 옵션)
- `pip install mlflow`만 추가 의존성
- 기존 `ml_service/server.py`와 `drl/api.py`의
  train/retrain 함수 끝에 `tracker.log_training()` 호출 추가
- Go용 `MLflowClient`는 REST API (`/api/2.0/mlflow/`) 사용

### 의존성 (Python)
```
mlflow>=2.10.0
```
