# Phase D — DRL (Deep Reinforcement Learning) 통합 명세서

**작성:** 헥스 (2026-04-11)
**대상:** 빅터스 (D1~D3), 헥스 (D4)
**우선순위:** P2 (Phase A~C 완료 후)

---

## 개요

FinRL-X 프레임워크 기반으로 PPO/SAC 에이전트를 구축하고,
기존 ML(LightGBM) 파이프라인과 병렬로 DRL 전략을 실행하여
양쪽의 성능을 비교 벤치마크합니다.

### 전체 아키텍처
```
Market Data
    ├── ML Pipeline (LightGBM) ──── Weight Vector (ML)
    │
    └── DRL Pipeline (PPO/SAC) ─── Weight Vector (DRL)
            │
            ├── Gym Environment (Binance Futures sim)
            ├── PPO Agent
            └── SAC Agent
                        │
                   Ensemble Layer ──→ 최종 Weight Vector
                        │
                   Risk Overlay ──→ 실행
```

---

## D1: FinRL-X PPO/SAC 에이전트 포팅

**산출물:** `internal/drl/agent.py`

### 역할
FinRL-X의 검증된 에이전트 아키텍처를 BigVolver에 맞게 포팅.
PPO (온라인 학습, 빠른 반응)와 SAC (오프라인 학습, 효율적 탐색) 두 가지 알고리즘 지원.

### 인터페이스
```python
class DRLAgent:
    """Deep Reinforcement Learning trading agent."""

    def __init__(
        self,
        env: "TradingEnv",
        algorithm: str = "ppo",      # "ppo" or "sac"
        policy_type: str = "MlpPolicy",
        tensorboard_log: str = "./drl_logs",
    ):
        """
        algorithm:
          - "ppo": Proximal Policy Optimization (빠른 수렴, 안정적)
          - "sac": Soft Actor-Critic (연속 액션, 샘플 효율적)
        """
        ...

    def train(
        self,
        total_timesteps: int = 100_000,
        callback_list = None,
    ) -> dict:
        """
        에이전트 학습.
        
        Returns:
            {
                "mean_reward": float,
                "std_reward": float,
                "episode_rewards": [float],
                "policy_loss": [float],
                "value_loss": [float],
            }
        """
        ...

    def predict(self, obs: np.ndarray) -> np.ndarray:
        """
        현재 관측 상태에서 액션(비중) 예측.
        
        Returns:
            np.ndarray of shape (n_assets,) — 각 자산에 대한 비중 [-1, 1]
        """
        ...

    def save(self, path: str) -> None:
        """모델 저장 (버전 포함)."""
        ...

    def load(self, path: str) -> None:
        """모델 로드."""
        ...

    def get_model_info(self) -> dict:
        """현재 모델 정보 반환."""
        ...
```

### 하이퍼파라미터 (PPO 기본값)
```python
PPO_DEFAULTS = {
    "learning_rate": 3e-4,
    "n_steps": 2048,
    "batch_size": 256,
    "n_epochs": 10,
    "gamma": 0.99,
    "gae_lambda": 0.95,
    "clip_range": 0.2,
    "ent_coef": 0.01,        # 탐험 장려
    "vf_coef": 0.5,
    "max_grad_norm": 0.5,
    "policy": "MlpPolicy",
    "policy_kwargs": {
        "net_arch": {"pi": [256, 256], "vf": [256, 256]},
    },
}

SAC_DEFAULTS = {
    "learning_rate": 3e-4,
    "buffer_size": 100_000,
    "learning_starts": 1000,
    "batch_size": 256,
    "tau": 0.005,             # soft update
    "gamma": 0.99,
    "ent_coef": "auto",       # 자동 엔트로피 조정
    "policy": "MlpPolicy",
}
```

### 주의사항
- **FinRL-X 의존성 최소화:** stable-baselines3 + gymnasium만 직접 사용.
  FinRL-X의 복잡한 래핑 대신 핵심 아이디어만 차용.
- **액션 스페이스:** Box(-1, 1, shape=(n_assets,)) — 연속 비중
- **관측 스페이스:** Box(shape=(n_features * n_assets,)) — 피처셋 평면화
- **리워드:** 샤프 기반 보상 함수 (Phase D2에서 정의)

---

## D2: Gym 환경 구축

**산출물:** `internal/drl/env.py`

### 역할
Binance Futures 거래를 시뮬레이션하는 OpenAI Gym 환경.
DRL 에이전트가 이 환경에서 학습하고 평가됨.

### 인터페이스
```python
class TradingEnv(gymnasium.Env):
    """Binance Futures trading environment for DRL."""

    metadata = {"render_modes": ["human", "none"]}

    def __init__(
        self,
        df: pd.DataFrame,          # OHLCV + features 데이터
        initial_balance: float = 10000,
        commission: float = 0.0004,  # 0.04% (Binance Futures taker)
        max_position: float = 1.0,
        reward_scheme: str = "sharpe",  # "sharpe", "return", "sortino"
        window_size: int = 50,       # 관측 윈도우
        verbose: bool = False,
    ):
        ...
        # Action space: Box(-1, 1) — -1=FULL SHORT, 0=FLAT, 1=FULL LONG
        self.action_space = spaces.Box(low=-1, high=1, shape=(1,))
        
        # Observation space: [window_size x n_features]
        self.observation_space = spaces.Box(
            low=-np.inf, high=np.inf,
            shape=(window_size, len(self.feature_cols)),
        )

    def reset(self, seed=None, options=None):
        """에피소드 리셋."""
        ...

    def step(self, action):
        """
        액션 실행.
        
        Args:
            action: np.ndarray — 비중 [-1, 1]
        
        Returns:
            observation, reward, terminated, truncated, info
        """
        ...

    def _calculate_reward(self) -> float:
        """
        리워드 계산.
        
        Sharpe-based reward (기본):
          - 최근 20스텝 수익률의 Sharpe ratio
          - 단기적 이익보다 리스크 조정 수익을 보상
          
        Sortino reward (선택):
          - 하방 변동성만 페널티
          - 트렌딩 시장에서 더 적합
        """
        ...

    def get_portfolio_metrics(self) -> dict:
        """현재 포트폴리오 메트릭 반환."""
        return {
            "total_return": float,
            "sharpe_ratio": float,
            "sortino_ratio": float,
            "max_drawdown": float,
            "win_rate": float,
            "total_trades": int,
            "equity_curve": [float],
        }
```

### 데이터 포맷
```python
# 입력 DataFrame columns (Phase A 피처셋과 동일)
feature_cols = [
    # Technical
    "ema_5", "ema_20", "ema_50", "ema_200",
    "rsi_14", "rsi_28", "macd_line", "macd_signal", "macd_histogram",
    "atr_14", "bollinger_upper", "bollinger_lower",
    "adx_14", "obv", "volume_ratio",
    # Microstructure
    "funding_rate", "funding_rate_change_1h", "funding_rate_change_8h",
    # Derived
    "volatility_1h", "volatility_4h", "volatility_24h",
    "momentum_1h", "momentum_4h", "mean_reversion_score",
    "regime_trending", "regime_ranging", "regime_volatile",
    # OHLCV
    "open", "high", "low", "close", "volume",
    # Target
    "future_return_4h",
]
```

### 리워드 함수 설계 (핵심)
```python
def sharpe_reward(equity_curve: list, risk_free_rate: float = 0.02) -> float:
    """
    Sharpe Ratio 기반 리워드.
    
    장점:
    - 리스크 조정 수익을 직접 최적화
    - 과도한 거래(수수료 손실) 자연스럽게 억제
    - 높은 변동성 전략 페널티
    
    계산:
      returns = equity_curve[t] / equity_curve[t-1] - 1
      sharpe = (mean(returns) - risk_free/hours) / std(returns) * sqrt(hours_per_year)
    """
    ...
```

---

## D3: DRL↔Go 브릿지

**산출물:** `internal/drl/bridge.go` + `internal/drl/api.py`

### 역할
Go 시스템에서 Python DRL 서비스와 통신.
기존 Predictor 패턴과 동일한 REST API 구조.

### Python API (`internal/drl/api.py`)
```python
# FastAPI 또는 Flask 서비스 (포트 5002)

app = Flask(__name__)

@app.route("/drl/health", methods=["GET"])
def health():
    """DRL 서비스 상태 확인."""
    ...

@app.route("/drl/predict", methods=["POST"])
def predict():
    """
    DRL 예측.
    
    Request:
        {
            "features": { "BTCUSDT": { "ema_5": ..., ... }, ... },
            "algorithm": "ppo" | "sac"
        }
    
    Response:
        {
            "weights": [
                { "symbol": "BTCUSDT", "weight": 0.5, "signal": "LONG" },
                ...
            ],
            "algorithm": "ppo",
            "model_version": "ppo-v20260411-...",
            "confidence": 0.8
        }
    """
    ...

@app.route("/drl/train", methods=["POST"])
def train():
    """
    DRL 학습 시작.
    
    Request:
        {
            "algorithm": "ppo",
            "timesteps": 100000,
            "symbol": "BTCUSDT",
            "data": [ ... ]  // JSONL records (optional, else load from file)
        }
    
    Response:
        {
            "success": true,
            "model_version": "ppo-v...",
            "mean_reward": float,
            "sharpe_ratio": float,
            "training_time_sec": float
        }
    """
    ...

@app.route("/drl/model/list", methods=["GET"])
def list_models():
    """사용 가능한 DRL 모델 목록."""
    ...

@app.route("/drl/model/load", methods=["POST"])
def load_model():
    """특정 버전의 DRL 모델 로드."""
    ...
```

### Go 브릿지 (`internal/drl/bridge.go`)
```go
// DRLBridge communicates with the Python DRL service
type DRLBridge struct {
    baseURL    string
    httpClient *http.Client
    modelVer   string
}

func NewDRLBridge(serviceURL string) *DRLBridge

// Predict sends features and returns DRL-generated weights
func (b *DRLBridge) Predict(symbols map[string]map[string]float64, algorithm string) (*DRLPrediction, error)

// Train triggers DRL training
func (b *DRLBridge) Train(req DRLTrainRequest) (*DRLTrainResponse, error)

// HealthCheck verifies the DRL service is alive
func (b *DRLBridge) HealthCheck() error

// LoadModel loads a specific model version
func (b *DRLBridge) LoadModel(version string) error
```

### DRLPrediction을 WeightVector로 변환
```go
func (b *DRLBridge) PredictToWeightVector(symbols map[string]map[string]float64, algorithm string) (*pipeline.WeightVector, error) {
    pred, err := b.Predict(symbols, algorithm)
    if err != nil {
        return nil, err
    }
    
    wv := pipeline.NewWeightVector(time.Now().Unix())
    for _, w := range pred.Weights {
        wv.AddWeight(pipeline.Weight{
            Symbol:    w.Symbol,
            Weight:    w.Weight,
            Confidence: pred.Confidence,
            Signal:    w.Signal,
            Source:    "drl_" + algorithm,
        })
    }
    return wv, nil
}
```

---

## D4: DRL vs ML 벤치마크 (헥스 담당)

**산출물:** `internal/drl/benchmark.py` + `BENCHMARK_REPORT.md`

### 벤치마크 프로토콜

#### 평가 조건
| 항목 | 설정 |
|------|------|
| 기간 | 최근 90일 |
| 종목 | BTCUSDT, ETHUSDT (기본) |
| 리밸런싱 | 4시간 (1h 캔들 기준) |
| 초기 자본 | $10,000 |
| 수수료 | 0.04% (taker, 양방향) |
| 슬리피지 | 0.05% |

#### 비교 모델
1. **LightGBM** (Phase A~C 파이프라인)
2. **PPO** (Deep RL)
3. **SAC** (Deep RL)
4. **Buy & Hold** (베이스라인)
5. **Ensemble** (ML + DRL 가중 평균)

#### 평가 메트릭
```python
METRICS = {
    "sharpe_ratio": "연간화 샤프 비율 (목표 > 1.5)",
    "sortino_ratio": "소르티노 비율 (하방 리스크만)",
    "max_drawdown": "최대 낙폭 (%) (목표 < 15%)",
    "calmar_ratio": "수익률 / MaxDD",
    "win_rate": "승률 (%) (목표 > 55%)",
    "profit_factor": "총 이익 / 총 손실",
    "total_return": "총 수익률 (%)",
    "annualized_return": "연간 수익률 (%)",
    "avg_trade_return": "평균 거래 수익률 (%)",
    "max_consecutive_losses": "최대 연속 손실",
    "sharpe_stability": "롤링 30일 Sharpe의 표준편차",
}
```

#### Walk-Forward 백테스트
```python
def walk_forward_benchmark(
    data: pd.DataFrame,
    models: list,              # ["lightgbm", "ppo", "sac"]
    train_window: int = 60,    # 학습 윈도우 (일)
    test_window: int = 30,     # 테스트 윈도우 (일)
    step: int = 15,            # 스텝 (일)
    rebalance_hours: int = 4,  # 리밸런싱 주기
) -> BenchmarkResult:
    """
    Walk-Forward 백테스트로 시간순 검증.
    
    각 fold:
      1. train_window 기간으로 학습
      2. test_window 기간으로 테스트
      3. 메트릭 기록
      4. step 만큼 슬라이드
    
    최종 결과: 모든 fold의 평균 메트릭 + 표준편차
    """
```

### 벤치마크 보고서 포맷
```markdown
# BigVolver DRL vs ML Benchmark Report

## 요약
| 모델 | Sharpe | Max DD | Win Rate | 총 수익률 | 연간 수익률 |
|------|--------|--------|----------|----------|-----------|
| LightGBM | ... | ... | ... | ... | ... |
| PPO | ... | ... | ... | ... | ... |
| SAC | ... | ... | ... | ... | ... |
| Ensemble | ... | ... | ... | ... | ... |
| Buy & Hold | ... | ... | ... | ... | ... |

## Walk-Forward 결과 (90일, 3-fold)
[각 fold별 메트릭 + 평균/표준편차]

## Equity Curves
[ equity curve 비교 그래프 ]

## Rolling Sharpe (30일)
[ 롤링 샤프 비교 ]

## 결론
[ 최고 성능 모델 + 선택 근거 + 권장 조합 ]
```

### 엔sembel 전략
```python
def ensemble_weights(
    ml_weights: dict,     # LightGBM 비중
    drl_weights: dict,    # DRL 비중
    ml_sharpe: float,     # ML 최근 Sharpe
    drl_sharpe: float,    # DRL 최근 Sharpe
) -> dict:
    """
    Sharpe 기반 가중 앙상블.
    
    최근 성능이 좋은 모델에 더 높은 가중치.
    
    weight_ml = ml_sharpe^2 / (ml_sharpe^2 + drl_sharpe^2)
    weight_drl = drl_sharpe^2 / (ml_sharpe^2 + drl_sharpe^2)
    
    final = weight_ml * ml + weight_drl * drl
    """
```

---

## Phase D 완료 조건

1. PPO 에이전트가 Gym 환경에서 학습 가능
2. SAC 에이전트가 Gym 환경에서 학습 가능
3. Go에서 DRL 예측 결과를 WeightVector로 변환 가능
4. Walk-Forward 백테스트로 ML vs DRL 비교 완료
5. 벤치마크 보고서 작성 + Telegram 전송

---

## 빅터스에게 전달

D1~D3 구현 시:
- DRL 서비스는 **포트 5002**에서 실행 (ML 서비스 5001과 분리)
- Go 브릿지는 기존 `Predictor`와 동일한 패턴으로 구현
- Gym 환경의 피처셋은 `config/features.yaml`과 완전히 동일해야 함
- 학습 데이터는 Phase A의 `ExportTrainingData()` JSONL 포맷 재사용

### 의존성 (Python)
```
stable-baselines3>=2.1.0
gymnasium>=0.29.0
numpy>=1.24.0
pandas>=2.0.0
torch>=2.0.0
flask>=3.0.0
```
