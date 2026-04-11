# BigVolver Phase A — Code Review (헥스)

**대상:** 빅터스 Phase A 커밋 (ed2189e)
**검토자:** 헥스
**일시:** 2026-04-11

---

## 전체 평가

Phase A 뼈대가 훌륭합니다. 4개 파일 1400줄로 핵심 구조를 모두 잡았고,
Go↔Python 브릿지 설계도 깔끔합니다. 아래 수정사항은 Phase A를
실제 동작 가능한 상태로 만들기 위한 것입니다.

---

## 🔴 버그 (수정 필수)

### 1. `calcMACDSignal()` — MACD Signal Line이 아님

**파일:** `internal/ml/features.go`
**문제:** 현재 구현은 close 가격의 9-period EMA를 반환.
이건 MACD Signal Line이 아니라 그냥 Price EMA입니다.

**정상 구현:**
```go
func calcMACDSignal(candles []*data.Candle) float64 {
    if len(candles) < 35 { // 26 + 9
        return 0
    }
    // MACD Line 시계열을 계산한 뒤, 그것의 9-period EMA
    macdLine := calcEMA(candles, 12) - calcEMA(candles, 26)
    // 하지만 이건 단일 포인트. 시계열이 필요함.
    // 별도 MACD 시계열 계산 함수 필요.
}
```

**해결 방안:** MACD Line의 시계열을 먼저 계산하고, 그 위에 EMA(9)를 적용하는
별도 함수 구현이 필요합니다. 참고: <https://en.wikipedia.org/wiki/MACD>

### 2. `randFloat()` — 항상 0.42 반환

**파일:** `internal/ml/walkforward.go`
**문제:** MockCandles()에서 randFloat()가 항상 0.42를 반환.
테스트 데이터가 전혀 랜덤하지 않음.

**수정:** `math/rand` 패키지 사용.
```go
import "math/rand"

func randFloat() float64 {
    return rand.Float64()
}
```

---

## 🟡 개선 권장

### 3. OBV 누적값 스케일 문제

**파일:** `internal/ml/features.go` — `calcOBV()`
**문제:** OBV는 누적값이라 시간이 지날수록 무한대로 커짐.
LightGBM은 tree-based라 영향은 적지만, feature importance가 왜곡될 수 있음.

**개선안:**
- `obv_normalized = obv / obv_sma(20)` 또는
- `obv_delta = obv_current - obv_prev_period`

### 4. ADX 단일 DX 반환

**파일:** `internal/ml/features.go` — `calcADX()`
**문제:** 완전한 ADX는 DX의 smoothed average (보통 14-period).
현재는 단일 DX 값만 반환.

**개선안:** DX 시계열을 계산하고 그 위에 Wilder smoothing 적용.
Phase A에서는 simplified로 유지해도 무방. Phase B에서 개선.

### 5. Python 서비스 `retrain` 엔드포인트 — 데이터 소스

**파일:** `ml_service/server.py`
**문제:** `/retrain`이 JSONL 파일을 읹지만, 파일이 어디서 오는지 명확하지 않음.
Go에서 `ExportTrainingData()`로 쓰고 Python에서 읽는 구조인데,
경로가 환경변수(`TRAINING_DATA_DIR`)로만 제어됨.

**개선안:**
- 기본 경로를 `./data/training_data_{symbol}.jsonl`로 명시
- Go 측에서 retrain 전에 데이터를 쓰는 API도 노출
- 또는 `/retrain`에 JSON body로 직접 데이터 전송 옵션 추가

### 6. Signal threshold 하드코딩

**파일:** `ml_service/server.py`
```python
if predicted_return > 0.3:    # LONG
elif predicted_return < -0.3:  # SHORT
```

**문제:** 0.3% threshold가 하드코딩됨. 시장 변동성에 따라 동적으로 조정되어야 함.

**개선안:** ATR 기반 동적 threshold 또는 config에서 관리.

---

## 🟢 잘한 점

1. **FeaturePipeline 구조** — Config-driven이라 피처 추가/제거가 유연함
2. **WalkForwardBacktest** — 시계열 교차검증을 제대로 구현
3. **ExportTrainingData** — Go에서 JSONL로 내보내고 Python에서 읹는 구조 깔끔
4. **Python 서비스 모델 버전관리** — generate_version()으로 자동 버전 부여
5. **SHAP-like importance** — predict 응답에 top 5 피처 중요도 포함

---

## 다음 단계 (헥스 → 빅터스)

**즉시 수정 요청 (P0):**
1. `calcMACDSignal()` 올바른 구현으로 교체
2. `randFloat()`에 `math/rand` 적용

**Phase B에서 함께 작업:**
3. `open_interest_change`, `long_short_ratio`, `top_trader_ls_ratio` 추가
4. OBV 정규화
5. 동적 signal threshold

---

## 헥스 완료 작업

- ✅ `config/features.yaml` — 공식 피처셋 정의서 작성 완료
- ✅ 코드 리뷰 완료
