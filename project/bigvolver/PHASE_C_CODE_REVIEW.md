# Phase C — Code Review (헥스)

**대상:** 빅터스 Phase C 커밋 (062b34e)
**검토자:** 헥스
**일시:** 2026-04-11

---

## 전체 평가

Weight-Centric 파이프라인이 잘 설계되었습니다. PipelineStage 인터페이스를
모든 모듈이 구현하고, PipelineChain으로 순차 실행하는 구조가 확장성이 좋습니다.

---

## ✅ 검증 완료

### C2: MLSelector
- confidence 기반 종목 선정 → 상위 N개 필터링 ✅
- LONG/SHORT → 양수/음수 비중 변환 ✅
- PipelineStage 인터페이스 정상 구현 ✅
- `ml.CurrentTimestamp()` 유틸리티 추가 (features.go) ✅

### C3: RiskOverlay
- 6단계 리스크 규칙 순차 적용 ✅
- 포지션 크기 제한 (30%) ✅
- 총 노출 제한 (1.0x) + 비례 축소 ✅
- 순 방향 제한 (0.5x) ✅
- ATR 기반 고변동 축소 ✅
- Max DD 서킷 브레이커 (15% 초과 → 0.5x) ✅
- Herfindahl 기반 포트폴리오 리스크 스코어 ✅
- Equity curve 추적 + DD 계산 ✅

### C4: TimingModule
- KAMA 완전 구현 (ER → SC → KAMA) ✅
- 진입/퇴출 버퍼 구역으로 비중 조절 ✅
- 트렌드 전환 시 비중 → 0 ✅
- KAMA/가격 메타데이터 저장 ✅

---

## 🟡 개선 권장 (Phase C 이후 또는 D에서)

### 1. DataWindow 코드 중복
`data_window.go`의 `computeFeatures()`, `computeIndicator()`,
`computeMicrostructure()`, `computeDerived()`가 `FeaturePipeline`과
거의 동일한 로직을 복제.

**개선안:** `FeaturePipeline`에 `ComputeFeaturesForCandles(candles)` 메서드를
추가하여 `DataWindow`가 재사용하도록 리팩토링.

### 2. MLSelector.Process()의 동작
현재 `Process()`는 전달받은 `WeightVector`의 심볼만 사용하지만,
`SelectSymbols()`를 호출하여 완전히 새로운 `WeightVector`를 반환.
기존 `WeightVector`의 데이터는 사라짐.

**개선안:** 타이밍/리스크가 이후에 붙는 구조이므로 현재 방식도 무방.
단, `Process()` 시 기존 metadata가 보존되는지 확인 필요.

### 3. RiskOverlay 동시성
`Process()` 내에서 `mu.Lock()`을 사용하나, `UpdateEquity()`도
별도로 Lock을 획득. 데드락은 없지만, `Process()`가 길게 Lock을 잡고 있으면
`UpdateEquity()`가 블록될 수 있음.

**개선안:** RLock으로 읽기 동작 분리. Phase D 이후 개선 충분.

---

## 🧪 테스트 커버리지 (헥스 작성)

`pipeline_test.go` — 16개 테스트 케이스:
- WeightVector: 생성, 추가, 조회, 정규화(3), 필터, 노출, 방향, 카운트
- PipelineChain: 다단계, 빈 체인, 에러 전파
- RiskOverlay: 포지션 제한, 총 노출, 순 방향, 고변동, DD 서킷 브레이커, 리스크 스코어, DD 계산
- 통합: 전체 파이프라인 체인

> 참고: MLSelector와 TimingModule 테스트는 DB 의존성 때문에 별도 모킹 필요.
> 현재 테스트는 Weight + RiskOverlay + PipelineChain에 집중.

---

## Phase C 검증: ✅ 통과

Phase D 명세서 작성 후 빅터스에게 전달 예정.
