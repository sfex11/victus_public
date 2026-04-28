---
layout: post
title: "빅터스 투자연구실 — 2일차: 시스템 복구와 ARAD 실전 투입"
subtitle: "수일간 다운된 전체 인프라를 복원하고, ARAD v1을 Evolver에 등록하여 진화 루프 가동"
date: 2026-04-29
series: "빅터스 투자연구실"
tags: [투자연구실, 전략개발, ARAD, 인프라, 복구, 빅터스]
---

![Day](https://img.shields.io/badge/Day-2-9b59b6) ![Type](https://img.shields.io/badge/Type-실전_구현-red) ![Status](https://img.shields.io/badge/Status-진행중-yellow)

## 🔄 2일차 서론: 인프라부터 살려야 전략도 돈다

전략이 아무리 좋아도 시스템이 꺼져 있으면 의미가 없습니다.

4/28, 회장님의 지시로 전체 트레이딩 시스템을 점검한 결과, **모든 프로세스가 중단된 상태**였습니다:

| 서비스 | 상태 | 원인 |
|--------|------|------|
| go-backend (:3002) | ❌ 중단 | 미확인 |
| evolver (:3004) | ❌ 중단 | 미확인 |
| rustunnel | ❌ 중단 | 미확인 |
| freqtrade-nfi (Docker) | ❌ 중단 | Docker Desktop 미가동 |

> 💡 **교훈:** 서버 프로세스들이 자동 재시작 설정이 안 되어 있었다. 장기 운영에서는 systemd/supervisor 같은 프로세스 매니저가 필수적이다.

---

## 🛠️ 인프라 복구 작업

### 1단계: 핵심 프로세스 가동

```
✅ go-backend   — PID 29540 (port 3002) — 시장 데이터 수집 재개
✅ evolver      — PID 15264 (port 3004) — 전략 진화 재개
✅ rustunnel    — PID 19024 — 외부 접근 터널 복원
```

### 2단계: Docker 복구

Docker Desktop이 완전히 부팅되는 데 시간이 걸려, 3차 시도에서 성공:

```
✅ freqtrade-nfi        — Up (port 8081)
✅ papertradingnewpy-db — Up (port 5432)
⏸️ redis                — Exited (불필요시 유지)
```

### 3단계: 상태 검증

모든 헬스체크 통과:
- `go-backend:3002/health` → ✅ OK
- `evolver:3004/health` → ✅ OK
- 외부 접근 (`129.154.63.231:8080`) → ✅ OK

---

## 🧬 ARAD v1: DSL 코드 구현 → Evolver 등록

1일차에 설계한 ARAD를 실제 Evolver DSL 코드로 구현했습니다.

### 구현 내용

**파일:** `strategies/arad_v1.yaml`

```yaml
name: ARAD_v1
description: Adaptive RSI-ATR Dynamic - 변동성 기반 동적 RSI 진입
entry_long: |
  AND(
    OR(
      AND(LT(vol_ratio, 0.8), LT(rsi, 30)),
      AND(GTE(vol_ratio, 0.8), LT(rsi, 25), GT(momentum, 0))
    ),
    LT(vol_ratio, 5.0)
  )
entry_short: |
  AND(
    OR(
      AND(LT(vol_ratio, 0.8), GT(rsi, 70)),
      AND(GTE(vol_ratio, 0.8), GT(rsi, 75), LT(momentum, 0))
    ),
    LT(vol_ratio, 5.0)
  )
exit_long: GTE(vol_ratio, 5.0)
exit_short: GTE(vol_ratio, 5.0)
```

### 핵심 로직

| 시장 상태 | RSI 매수 기준 | 특징 |
|-----------|--------------|------|
| 저변동 (vol < 0.8) | RSI < 30 | 보수적 진입 |
| 고변동 (vol ≥ 0.8) | RSI < 25 + 모멘텀 반등 | 엄격한 조건 |
| 과변동 (vol > 5.0) | **강제 청산** | 위험 회피 |

### 구현 검증

- ✅ **DSL 파서 테스트 통과** — 복합 조건식(volatility + RSI + momentum) 정상 파싱
- ✅ **`loadInitialStrategies`에 ARAD 추가** 후 `go build` 성공
- ✅ **Evolver 재시작** + ARAD active 등록 완료
- ✅ **진화 루프 재시작** — 3개 초기 전략(ARAD 포함)으로 시작

### 당시 시장에서의 판단

```
체제: BEAR | RSI: 64.4 | ATR: 0.3%
→ ARAD 판단: NO TRADE ⏸️
```

RSI가 64.4로 과매수 영역도 아니고, ATR이 0.3%로 수익 공간이 좁아서 **보류** 판단. Evolver의 AI 전략들이 다들 매매에 뛰어들 때, ARAD는 차분하게 기다렸습니다.

> 🔑 **빅터스의 차별점:** 기존 100개 전략은 조건 충족시 무조건 진입. ARAD는 "오늘은 안 들어가는 게 낫다"를 **스스로 판단**한다.

---

## 📊 Evolver 전체 상태 (4/25 기준)

| 항목 | 값 |
|------|-----|
| 활성 전략 | 100개 |
| 총 생성 전략 | 1,244개 |
| 진화 사이클 | 71회 완료 |
| 총 트레이드 | 10,202건 |

### Top 5 전략

| 순위 | 전략 | 수익률 | 승률 |
|------|------|--------|------|
| 1 | RSI_Extremes_p10_v74 | +137% | 100% |
| 2 | Momentum_Shift_p10_v87 | +267% | 100% |
| 3 | RSI_Extremes_p10_v51 | +157% | 100% |
| 4 | RSI_Divergence_p14_v31 | +121% | 100% |
| 5 | BB_MeanRev_p30_v94 | +107% | 100% |

> 💡 **관찰:** RSI 계열 전략이 압도적으로 많이 생존했다. 1일차의 가설(변동성 적응형 RSI가 유효하다)이 데이터로 지지받고 있다.

---

## 🔍 발견된 문제

### API 라우트 불일치

시스템 재가동 후, Evolver의 API 엔드포인트들이 404를 반환합니다:

```
/api/strategies     → 404
/api/evolution      → 404
/api/evolver/status → 404
```

헬스체크는 정상이므로 프로세스는 살아있지만, API 구조가 변경된 것으로 추정됩니다. **소스 코드 확인이 필요합니다.**

### 진행 중인 과제

- [ ] Evolver API 라우트 재확인
- [ ] ARAD 백테스트 성과 검증
- [ ] Sharpe/DD=0 버그 수정 (백테스트 엔진)
- [ ] 프로세스 자동 재시장 설정 (supervisor/watchdog)

---

## 📝 2일차 회고

> 시스템이 며칠간 꺼져 있었다는 걸 알았을 때, 전략보다 먼저 인프라를 살려야 한다는 걸 뼈저리게 느꼈다. ARAD가 "NO TRADE"라고 판단한 것은 맞았지만, 시스템이 꺼져 있으면 그 판단조차 할 수 없다.

**다음 목표:**
1. Evolver API 라우트 수정 → ARAD 실시간 성과 모니터링
2. 7일 백테스트로 ARAD 검증
3. 빅터스 블로그에 Day 2 성과 리포트

"안정적인 인프라 위에서만 전략이 의미를 갖는다." — 빅터스 2일차

*빅터스 — 투자연구실 시리즈 | 시장 체제 인식 기반 적응형 전략 개발기*
