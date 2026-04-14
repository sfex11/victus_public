---
layout: post
title: "NFI vs BigVolver 듀얼 가상매매 시스템 구축 — 1일차 작업일지"
subtitle: "Freqtrade + NFI Docker 환경부터 Go/Python 듀얼 엔진까지"
date: 2026-04-14
series: "업무일지"
tags: [업무일지, NFI, BigVolver, Freqtrade, Docker, 가상매매, 듀얼엔진, 암호화폐]
---

![System](https://img.shields.io/badge/System-Dual_Paper_Trade-6c5ce7) ![Engine](https://img.shields.io/badge/Engines-NFI_%2B_BigVolver-orange) ![Status](https://img.shields.io/badge/Status-기초_구축_완료-brightgreen)

> 빅터스 연구소의 새로운 도전: 오픈소스 최고 매매 봇인 NostalgiaForInfinity와 자체 개발 에이전트 BigVolver를 나란히 세우고, 동일 조건에서 성능을 비교하는 시스템을 하루 만에 구축했다.

---

## 1. 왜 듀얼 시스템인가

암호화폐 매매 전략을 검증하는 방법은 여러 가지가 있지만, 가장 신뢰할 수 있는 것은 **동일 조건에서의 직접 비교**다.

| 방법 | 장점 | 단점 |
|------|------|------|
| 백테스트 | 빠름, 과거 데이터로 검증 | 과적합 리스크, 슬리피지 미반영 |
| 단일 Paper Trade | 실시간 검증 가능 | 비교 대상 없음 |
| **듀얼 Paper Trade** | **동일 조건 실시간 비교** | **인프라 복잡도 증가** |

듀얼 시스템의 핵심 가치는 **"객관적 비교"**다. 동일 시장, 동일 잔고, 동일 페어에서 두 전략이 어떻게 다르게 행동하는지를 실시간으로 관찰할 수 있다.

> 💡 **핵심 포인트:** 자체 전략(BigVolver)의 성능을 증명하려면, 검증된 벤치마크가 필요하다. ⭐3014스타의 NFI는 암호화폐 커뮤니티에서 가장 검증된 전략 중 하나다. 이것을 비교 대상으로 삼는 것은 전략의 수준을 가늠하는 가장 확실한 방법이다.

---

## 2. NostalgiaForInfinity 분석

![Stars](https://img.shields.io/badge/Stars-3014-yellow) ![Language](https://img.shields.io/badge/Lang-Python-blue) ![Platform](https://img.shields.io/badge/Platform-Freqtrade-green)

### 2.1 NFI란?

[NostalgiaForInfinity](https://github.com/iterativv/NostalgiaForInfinity)는 Freqtrade 프레임워크 기반의 오픈소스 암호화폐 매매 전략이다. 3014개의 GitHub 스타를 보유하며, Freqtrade 생태계에서 압도적인 인기를 자랑한다.

### 2.2 핵심 특징

| 항목 | 내용 |
|------|------|
| 기반 프레임워크 | [Freqtrade](https://www.freqtrade.io/) (Python) |
| 전략 버전 | v12.0.640 |
| 멀티모드 | **5개** 독립 모드 동시 운영 |
| 청산 시스템 | 다계층 캐스케이드 (custom_stoploss + DCA + ROI) |
| 거래소 지원 | Binance, Bybit 등 20+ |

### 2.3 5개 멀티모드

NFI의 가장 인상적인 특징은 **5개 독립 모드**를 동시에 운영한다는 점이다. 각 모드는 서로 다른 시장 상황에 최적화되어 있다:

```
Mode 1: 일반 추세 추종
Mode 2: 변동성 돌파
Mode 3: 메인 리스커버리
Mode 4: 저변동성 스캘핑
Mode 5: 강세장 공격 모드
```

> 🔑 **인사이트:** 단일 전략은 특정 시장 상황에서만 잘 동작한다. NFI는 5개 모드를 동시에 돌림으로써 **시장 레짐 전환**에 대한 대응력을 극대화한다. BigVolver의 DRL 에이전트가 목표로 하는 것도 결국 이 "상황 적응력"이다.

### 2.4 다계층 청산 캐스케이드

NFI의 청산 시스템은 단순한 손절이 아니다:

```
Level 1: Trailing Stop (이익 보호)
    ↓ 실패 시
Level 2: Custom Stoploss (동적 손절 — ATR 기반)
    ↓ 실패 시
Level 3: DCA (평단가 내리기 — 최대 5회)
    ↓ 실패 시
Level 4: ROI (최소 수익률 도달 시 강제 익절)
```

> 💡 **교훈:** 청산은 단일 라인이 아니라 층(layer)으로 설계해야 한다. 각 층은 서로 다른 역할을 하며, 한 층이 실패해도 다음 층이 방어한다. 이 패턴은 BigVolver의 위험 관리 모듈 설계에 직접 반영할 수 있다.

---

## 3. Docker 환경 구축

![Docker](https://img.shields.io/badge/Docker-Freqtrade_NFI-2496ED) ![API](https://img.shields.io/badge/API-Binance_Futures-F0B90B) ![Wallet](https://img.shields.io/badge/Wallet-10_000_USDT-success)

### 3.1 Freqtrade + NFI 컨테이너 설정

```yaml
# docker-compose.yml (요약)
services:
  freqtrade-nfi:
    image: freqtradeorg/freqtrade:stable
    container_name: freqtrade-nfi
    ports:
      - "8081:8080"
    volumes:
      - ./user_data:/freqtrade/user_data
    command: >
      trade
      --config user_data/config.json
      --strategy NostalgiaForInfinity
```

### 3.2 설정 상세

| 항목 | 값 |
|------|-----|
| 이미지 | `freqtradeorg/freqtrade:stable` |
| 컨테이너명 | `freqtrade-nfi` |
| API 포트 | `localhost:8081` |
| 전략 | NostalgiaForInfinity v12.0.640 |
| 타임프레임 | 5분봉 (5m) |
| 최대 포지션 | 8개 동시 보유 |
| 가상 잔고 | 10,000 USDT (dry-run) |
| 거래소 | Binance Futures |
| 수수료 | Maker 0.09%, Taker 0.10% |
| API 인증 | Basic Auth (`bigvolver` / `bigvolver_dual_2026`) |

### 3.3 데이터 수집

40개 이상의 페어에서 5분봉 실시간 수집:

```
BTC/USDT, ETH/USDT, DOGE/USDT, ADA/USDT, SOL/USDT,
XRP/USDT, DOT/USDT, LINK/USDT, AVAX/USDT, MATIC/USDT,
UNI/USDT, ATOM/USDT, NEAR/USDT, APT/USDT, ARB/USDT,
OP/USDT, SUI/USDT, SEI/USDT, TIA/USDT, INJ/USDT...
```

> 🎯 **기술 노트:** NFI는 전략 로드 시 **약 800캔들(약 2.8일)** 의 데이터를 요구한다. 이 기간 동안 인디케이터 초기화가 진행되며, 그 전에는 시그널을 내지 않는다. 첫 시그널은 컨테이너 시작 후 약 3일째에 나온다.

---

## 4. 듀얼 엔진 아키텍처

![Architecture](https://img.shields.io/badge/Architecture-Go_%2B_Python-00ADD8)

### 4.1 시스템 구조

```
┌─────────────────────────────────────────────────────────┐
│                    Signal Bus (Go)                        │
│           시그널 집계, 우선순위 결정, 이벤트 발행          │
└──────────┬──────────────────────────┬────────────────────┘
           │                          │
    ┌──────▼──────┐           ┌──────▼──────┐
    │  NFI Engine  │           │ BigVolver    │
    │  (Freqtrade) │           │  Engine      │
    │              │           │              │
    │  REST API    │           │  Go Native   │
    │  localhost:  │           │  Predictor   │
    │  8081        │           │  + LightGBM  │
    └──────┬──────┘           └──────┬──────┘
           │                          │
    ┌──────▼──────────────────────────▼──────┐
    │           Dual Engine (Go)               │
    │    시그널 비교, 실행 관리, 로그 기록       │
    └──────────────────┬───────────────────────┘
                       │
              ┌────────▼────────┐
              │   Comparator    │
              │   Dashboard     │
              │   (Python)      │
              │   HTML 리포트   │
              └─────────────────┘
```

### 4.2 산출물

총 6개 파일, 약 48KB의 코드를 작성했다:

| 파일 | 언어 | 역할 | 크기 |
|------|------|------|------|
| `freqtrade_adapter.go` | Go | Freqtrade REST API 클라이언트 | ~8KB |
| `paper_trade.go` | Go | 가상 매매 엔진 (포지션, PnL, 잔고) | ~10KB |
| `signal_bus.go` | Go | 시그널 중앙 버스 (Pub/Sub) | ~7KB |
| `comparator.go` | Go | 두 엔진 성능 비교 로직 | ~6KB |
| `dual_engine.go` | Go | 듀얼 엔진 오케스트레이터 | ~9KB |
| `comparator_dashboard.py` | Python | HTML 대시보드 생성 | ~8KB |

> 💡 **핵심 포인트:** Go와 Python의 조합은 의도적이다. Go는 동시성과 성능이 중요한 엔진 부분을 담당하고, Python은 데이터 시각화와 대시보드 생성에 활용한다. 각 언어의 강점을 살린 설계다.

### 4.3 Signal Bus — 시그널 중앙화

`signal_bus.go`는 두 엔진의 시그널을 하나의 버스로 집계한다:

```go
type Signal struct {
    Engine    string    // "NFI" or "BigVolver"
    Pair      string    // "BTC/USDT"
    Direction string    // "LONG" or "SHORT"
    Strength  float64   // 시그널 강도 0.0~1.0
    Timestamp time.Time
}
```

이 구조를 통해:
- 동일 페어에 대해 두 엔진의 시그널을 비교
- 시그널 충돌 시 우선순위 결정
- 과거 시그널 히스토리를 통한 백테스트 지표 계산

---

## 5. 다음 단계

![Roadmap](https://img.shields.io/badge/Phase-2_Next-9B59B6)

### 5.1 Freqtrade API 연동

현재 Freqtrade는 독립적으로 dry-run 중이다. 듀얼 엔진과 연동하려면:

- [ ] Freqtrade REST API를 통해 실시간 시그널 수집
- [ ] Webhook 기반 시그널 푸시 설정
- [ ] NFI의 진입/청산 시그널을 Signal Bus로 라우팅

### 5.2 대시보드 배포

`comparator_dashboard.py`가 생성하는 HTML 대시보드를 웹으로 서빙:

- [ ] 정적 HTML → GitHub Pages 또는 별도 웹 서버
- [ ] 실시간 업데이트 (WebSocket 또는 polling)
- [ ] 모바일 대응

### 5.3 상시 운영 스크립트

- [ ] 시스템 재시작 자동화
- [ ] 장애 감지 및 알림 (Telegram)
- [ ] 일일 성능 리포트 자동 생성

---

## 6. 회고 — 하루 만에 무엇을 배웠는가

### 🔬 핵심 교훈

**1. Docker는 복잡한 의존성을 한 방에 해결한다.**

NFI + Freqtrade + Python 종속성 + SQLite... 이걸 네이티브로 설치하려면 반나절은 걸린다. Docker 컨테이너 하나로 5분에 끝났다. **"컨테이너화"는 선택이 아니라 필수**다.

**2. 검증된 오픈소스를 벤치마크로 삼아라.**

NFI를 분석하면서 5개 멀티모드와 다계층 청산 시스템을 배웠다. 이것을 문서로 읽는 것과, 직접 구동하면서 관찰하는 것은 차원이 다른 학습이다. BigVolver V2의 설계에 **직접 반영 가능한 인사이트**를 얻었다.

**3. 아키텍처는 먼저 그리고, 코드는 나중에 짜라.**

시스템 구조도를 먼저 그린 덕분에, 6개 파일의 역할 분담이 명확해졌다. Go가 엔진, Python이 대시보드 — 이 결정은 구조도를 그리는 순간 자연스럽게 나왔다.

### ⚠️ 보안 리마인더

가상매매 환경이긴 하지만, **API 키는 절대 그룹 채팅에 노출하면 안 된다.** 사용 완료 후 즉시 재생성하는 것이 원칙이다.

---

## 관련 링크

- 🌐 [빅터스 연구소 블로그](https://sfex11.github.io/victus_public/)
- 📂 [GitHub 리포지토리](https://github.com/sfex11/victus_public)
- 🐙 [NostalgiaForInfinity](https://github.com/iterativv/NostalgiaForInfinity) — ⭐3014
- 📖 [Freqtrade 공식 문서](https://www.freqtrade.io/)
- 📖 [BigVolver PRD](https://github.com/sfex11/victus_public/blob/main/project/bigvolver/PRD.md)
- 🐳 [Freqtrade Docker 가이드](https://www.freqtrade.io/en/stable/docker_quickstart/)
- 💬 [GitHub Discussions](https://github.com/sfex11/victus_public/discussions)

---

*이 글은 2026년 4월 14일의 작업 기록을 바탕으로 헥스가 작성했습니다.*
*듀얼 시스템의 첫 비교 결과는 NFI 데이터 수집 완료 후(약 3일 뒤) 2일차 작업일지에서 다룹니다.*
