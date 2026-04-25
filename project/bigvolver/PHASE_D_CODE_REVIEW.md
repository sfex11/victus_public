# Phase D — Code Review (헥스)

**대상:** 빅터스 Phase D 커밋 (e7f8fa6)
**검토자:** 헥스
**일시:** 2026-04-11

---

## 전체 평가

DRL 통합이 파이프라인 아키텍처와 일관되게 설계되었습니다.
ML 서비스(5001)와 DRL 서비스(5002)의 분리, Predictor/DRLBridge의
패턴 일치, WeightVector로의 변환까지 전부 잘 연결됩니다.

---

## ✅ 검증 완료

### D1: DRLAgent (agent.py)
- PPO + SAC 듀얼 알고리즘 지원 ✅
- 하이퍼파라미터 명세서와 일치 ✅
- 버전 관리 (algorithm-v{timestamp}) ✅
- save/load + 메타데이터 JSON ✅
- 학습 후 자동 평가 (3 에피소드) ✅

### D2: TradingEnv (env.py)
- Sharpe 기반 보상 (clamped [-3, 3]) ✅
- Sortino 보상 옵션 ✅
- 0.04% 수수료 + 0.05% 슬리피지 ✅
- 청산 보호 (자본 10% 이하 → terminated) ✅
- FEATURE_COLS가 config/features.yaml과 일치 ✅
- 랜덤 시작 포지션 (과적합 방지) ✅
- get_portfolio_metrics()로 종합 평가 ✅

### D3: DRL API + Go Bridge
- Python API :5002 ✅
  - /drl/health, /drl/predict, /drl/train, /drl/model/list, /drl/model/load
- Go DRLBridge ✅
  - 600s 타임아웃 (학습 시간 고려)
  - Predictor와 동일한 패턴
  - PredictToWeightVector 변환 가능 (명세서 참조)

---

## 🟡 개선 권장

### 1. TradingEnv 포지션 체결 로직
현재 `position_change > 0.01`일 때만 비용 계산.
`entry_price` 업데이트가 포지션 오픈 시에만 일어남.
클로징 시 `pnl` 계산에 `self.balance`를 곱하는데,
balance는 업데이트되지 않아 equity curve와 불일치 가능.

**영향:** 보상 함수 자체는 equity curve 기반이라 정확.
pnl/win_rate만 부정확 → 주요 메트릭에는 영향 없음.

### 2. DRL API의 predict에서 observation 구성
단일 feature dict를 받으면 env._get_observation()를 사용.
env가 어떤 데이터로 초기화되었는지에 따라 obs가 달라짐.
predict 시점에 최신 데이터를 사용하는지 확인 필요.

### 3. agent.py에서 lazy import
`stable_baselines3`을 `train()`과 `load()` 안에서 import.
학습하지 않고 predict만 하는 환경에서는 SB3 의존성 불필요 → 좋은 설계.

---

## 📋 D4: Benchmark (헥스 완료)

### 산출물
- `internal/drl/benchmark.py`

### 기능
- **11개 평가 메트릭:** Sharpe, Sortino, Max DD, Calmar, Win Rate,
  Profit Factor, Total/Annualized Return, Avg Trade Return,
  Max Consecutive Losses, Sharpe Stability
- **Walk-Forward 백테스트:** train 60일 / test 30일 / step 15일
- **5개 모델 비교:** Buy & Hold, LightGBM, PPO, SAC, Ensemble
- **Sharpe² 가중 앙상블**
- **Markdown 보고서 자동 생성**
- **CLI 인터페이스:** `python benchmark.py --data features.csv`

### 플레이스홀더
현재 실제 모델 연결 전이므로, ML/DRL 시그널에 휴리스틱 사용.
실제 모델 연결 시 `generate_placeholder_*_signals()` 함수만 교체하면 됨.

---

## Phase D 검증: ✅ 통과

다음은 Phase E (실험 추적 & 모니터링).
