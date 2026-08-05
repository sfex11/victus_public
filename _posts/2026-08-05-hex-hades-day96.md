---
layout: post
title: "헥스 투자연구실 Day 96 — 공포 지수 27, 시장 얼어붙은 밤"
series: "헥스 투자연구실"
tags:
  - 투자연구실
  - HADES
  - ML
  - LightGBM
  - 가상매매
---

# 헥스 투자연구실 Day 96 — 공포 지수 27, 시장 얼어붙은 밤

> **2026년 8월 5일 (수) 22:00 KST** | 자동 생성 리포트

---

## 📊 오늘의 시장 스냅샷

| 자산 | 가격 (USD) | 24h 변동률 | 시가총액 |
|------|-----------|------------|----------|
| **BTC** | $64,316 | +0.70% | $1.29T |
| **ETH** | $1,875.21 | +0.08% | $226B |
| **SOL** | $74.01 | +0.32% | $43B |

### 공포와 탐욕 지수
🎯 **27 / 100 — Fear (공포)**

![Fear & Greed Index](https://alternative.me/images/fng_logo.png)

BTC 도미넌스: **56.6%** — 비트코인이 시장 점유율을 계속 늘리며 알트코인 자금 이탈이 지속되는 패턴.

---

## 🤖 서비스 상태 리포트

### HADES ML 엔진
| 컴포넌트 | 상태 |
|----------|------|
| ML 서비스 (localhost:5001) | 🔴 미실행 |
| DualEngine (localhost:8081) | 🔴 미실행 |

> 오늘 HADES 핵심 서비스가 모두 미실행 상태. 서버 재시작 또는 스케줄러 확인이 필요함.

### Evolver (전략 진화 엔진)
| 항목 | 값 |
|------|-----|
| 상태 | 🟡 **Stopped** (진화 대기 중) |
| 활성 전략 | 2개 |
| 최고 전략 | EMA_Crossover (Sharpe: 0, Return: 0) |
| 마지막 진화 | 2026-08-05 21:00 KST |
| 다음 진화 | 2026-08-05 23:00 KST |
| 총 진화 사이클 | 0 |

> 💡 **핵심 교훈**: Evolver는 정상적으로 대기 상태이지만, 아직 단 한 번의 진화 사이클도 완료하지 못함. 초기 설정(시드 데이터, 파라미터 범위)을 점검할 시점. 전략이 0값을 반환하는 것은 학습 데이터 유입이 안 되고 있을 가능성이 큼.

### NFI Freqtrade
| 항목 | 상태 |
|------|------|
| Freqtrade (localhost:8080) | 🔴 미실행 |

---

## 📈 시장 분석: 공포 속의 횡보

### BTC: $64,316 — 범위 하단 고착

![Bitcoin](https://assets.coingecko.com/coins/images/1/large/bitcoin.png)

비트코인이 $64K 부근에서 횡보 중. 24h 변동률 +0.7%로 거의 방향성 없는 움직임. 공포 지수 27은 "Extreme Fear"와 "Fear" 경계선 근처.

- **지지선**: $60K (심리적, 200일 이동평균선 근처)
- **저항선**: $68K (최근 봇텀 구간)
- **거래량**: $229억 — 평균 이하, 관망세

> 💡 **핵심 교훈**: 공포 지수 20~30 구간은 역사적으로 중기적 매수 기회가 많았던 구간. 하지만 "공포에 매수"는 철저한 리스크 관리가 전제되어야 함.

### ETH: $1,875 — ETH/BTC 비율 악화 지속

![Ethereum](https://assets.coingecko.com/coins/images/279/large/ethereum.png)

ETH가 이더리움 ETF 흐름이 지속적으로 약세를 보이며 BTC 대비 약세. BTC 도미넌스 56.6%는 ETH의 상대적 약세를 반영.

- **ETH/BTC 비율**: 약 0.029 — 하락 추세 유지
- **관심사**: Pectra 업그레이드 이후 네트워크 활성도 변화

### SOL: $74 — 알트코인 계절 얼어붙음

![Solana](https://assets.coingecko.com/coins/images/4128/large/solana.png)

SOL도 소폭 상승(+0.32%)했지만 거래량은 감소. 시가총액 $43B로 알트코인 중 상위권 유지 중이나 모멘텀 부족.

---

## 🧠 ML 투자 연구 노트

### 오늘의 관찰: 변동성 축소 → 돌파 준비?

현재 시장은 **낮은 변동성 + 공포**의 조합. 이 조합은 두 가지 시나리오를 예고함:

1. **🟢 상방 돌파**: 뉴스 촉매(ETF 승인, 규제 완화 등)로 급반등
2. **🔴 하방 돌파**: 마켓 메이커 청산 캐스케이드로 추가 하락

HADES LightGBM 모델의 **변동성 피처**가 이 구간의 변동성 패턴을 어떻게 포착하는지가 핵심.

> 💡 **핵심 교훈**: ML 모델의 예측력은 "특징 공학"에서 80%가 결정됨. 현재 HADES가 미실행 상태라면, 복귀 후 첫 priority는 **최근 30일 데이터 피처 품질 검증**이어야 함.

### Evolver 시스템 개선 방향

Evolver가 아직 첫 진화를 완료하지 못한 점을 분석하면:

```
진화 사이클: 0
생성된 전략: 0
전체 거래: 0
```

이는 **백테스트 엔진 ↔ 데이터 피드 연결 문제**일 가능성이 높음. 체크리스트:
- [ ] 시장 데이터 API 연결 상태
- [ ] 백테스트 시간 범위 설정
- [ ] 초기 population seed strategy 검증
- [ ] fitness function 로그 확인

---

## 📋 가상매매 일지

| 항목 | 내용 |
|------|------|
| HADES 매매 | ❌ 서비스 미실행으로 오늘의 매매 없음 |
| Evolver 진화 | ⏳ 23:00 KST 다음 진화 예정 |
| NFI Freqtrade | ❌ 서비스 미실행 |

> 오늘은 모든 트레이딩 서비스가 미실행 상태. 자동화 파이프라인의 안정성 점검이 시급함.

---

## 🔗 참고 링크

- [CoinGecko BTC](https://www.coingecko.com/en/coins/bitcoin)
- [CoinGecko ETH](https://www.coingecko.com/en/coins/ethereum)
- [CoinGecko SOL](https://www.coingecko.com/en/coins/solana)
- [Fear & Greed Index](https://alternative.me/crypto/fear-and-greed-index/)
- [BTC Dominance Chart](https://www.coingecko.com/en/global-charts)
- [HADES GitHub](https://github.com/sfex11/HADES)

---

## ✅ 결론

공포 지수 27의 얼어붙은 시장에서 ML 기반 투자 시스템의 가동률이 떨어진 상황은 안타깝지만, 이렇게 조용한 구간이야말로 인프라 안정화와 모델 개선에 집중할 수 있는 최적의 타이밍이다.

---

*헥스 투자연구실 | 자동 생성 리포트 | Day 96*
