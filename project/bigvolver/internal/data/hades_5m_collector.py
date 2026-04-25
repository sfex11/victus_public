"""
HADES 5분봉 데이터 수집기 — Binance Futures에서 5분봉 OHLCV 수집

Usage:
    # 초기 백필 (90일)
    python hades_5m_collector.py --symbols BTCUSDT ETHUSDT SOLUSDT --days 90 --db bigvolver.db

    # 실시간 수집
    python hades_5m_collector.py --symbols BTCUSDT ETHUSDT SOLUSDT --live --db bigvolver.db

    # 상태 확인
    python hades_5m_collector.py --stats --db bigvolver.db
"""

import argparse
import json
import sqlite3
import sys
import time
from datetime import datetime, timedelta, timezone
from pathlib import Path

BINANCE_BASE = "https://fapi.binance.com"
KST = timezone(timedelta(hours=9))
DEFAULT_SYMBOLS = ["BTCUSDT", "ETHUSDT", "SOLUSDT"]
DEFAULT_DAYS = 90


class Hades5mCollector:
    """Binance Futures 5분봉 OHLCV 수집기"""

    def __init__(self, db_path: str = "bigvolver.db"):
        self.db_path = db_path
        self.conn = sqlite3.connect(db_path)
        self.conn.execute("PRAGMA journal_mode=WAL")
        self.conn.execute("""
            CREATE TABLE IF NOT EXISTS market_5m_candles (
                symbol TEXT NOT NULL,
                timestamp INTEGER NOT NULL,
                open REAL NOT NULL,
                high REAL NOT NULL,
                low REAL NOT NULL,
                close REAL NOT NULL,
                volume REAL NOT NULL,
                PRIMARY KEY (symbol, timestamp)
            )
        """)
        self.conn.execute("CREATE INDEX IF NOT EXISTS idx_5m_symbol_ts ON market_5m_candles(symbol, timestamp)")
        self.conn.commit()

    def close(self):
        if self.conn:
            self.conn.close()

    def fetch_klines(self, symbol: str, start_time: int = None, end_time: int = None, limit: int = 1500) -> list:
        url = f"{BINANCE_BASE}/fapi/v1/klines"
        params = {"symbol": symbol, "interval": "5m", "limit": limit}
        if start_time:
            params["startTime"] = start_time
        if end_time:
            params["endTime"] = end_time

        import urllib.request
        query = "&".join(f"{k}={v}" for k, v in params.items())
        req = urllib.request.Request(f"{url}?{query}")

        try:
            with urllib.request.urlopen(req, timeout=30) as resp:
                data = json.loads(resp.read().decode())
        except Exception as e:
            print(f"[HADES 5m] Failed to fetch {symbol}: {e}")
            return []

        candles = []
        for k in data:
            candles.append({
                "symbol": symbol,
                "timestamp": int(k[0] // 1000),
                "open": float(k[1]),
                "high": float(k[2]),
                "low": float(k[3]),
                "close": float(k[4]),
                "volume": float(k[5]),
            })
        return candles

    def backfill(self, symbol: str, days: int = 90) -> int:
        end_time = int(datetime.now(timezone.utc).timestamp() * 1000)
        start_time = int((datetime.now(timezone.utc) - timedelta(days=days)).timestamp() * 1000)
        total = 0
        current = start_time

        while current < end_time:
            candles = self.fetch_klines(symbol, start_time=current, end_time=end_time, limit=1500)
            if not candles:
                break

            for c in candles:
                try:
                    self.conn.execute("""
                        INSERT OR REPLACE INTO market_5m_candles
                        (symbol, timestamp, open, high, low, close, volume)
                        VALUES (?, ?, ?, ?, ?, ?, ?)
                    """, (c["symbol"], c["timestamp"], c["open"], c["high"],
                          c["low"], c["close"], c["volume"]))
                except Exception as e:
                    print(f"  DB error: {e}")
            self.conn.commit()
            total += len(candles)

            last_ts = candles[-1]["timestamp"] * 1000 + 1
            if last_ts <= current:
                break
            current = last_ts

            progress = min(100, (current - start_time) / (end_time - start_time) * 100)
            sys.stdout.write(f"\r  {symbol}: {progress:.0f}% ({total} candles)")
            sys.stdout.flush()
            time.sleep(0.2)

        sys.stdout.write(f"\r  {symbol}: OK {total} candles backfilled          \n")
        sys.stdout.flush()
        return total

    def collect_live(self, symbols: list, interval_seconds: int = 300):
        print(f"[HADES 5m] Live collection started. Symbols: {symbols}")
        print(f"[HADES 5m] Polling every {interval_seconds}s. Press Ctrl+C to stop.")

        while True:
            try:
                for symbol in symbols:
                    candles = self.fetch_klines(symbol, limit=1)
                    if candles:
                        for c in candles:
                            self.conn.execute("""
                                INSERT OR REPLACE INTO market_5m_candles
                                (symbol, timestamp, open, high, low, close, volume)
                                VALUES (?, ?, ?, ?, ?, ?, ?)
                            """, (c["symbol"], c["timestamp"], c["open"], c["high"],
                                  c["low"], c["close"], c["volume"]))
                        self.conn.commit()

                now = datetime.now(KST).strftime("%H:%M:%S")
                sys.stdout.write(f"\r  [{now}] Collected 5m data for {len(symbols)} symbols")
                sys.stdout.flush()
                time.sleep(interval_seconds)
            except KeyboardInterrupt:
                print("\n[HADES 5m] Stopped.")
                break

    def get_latest_candles(self, symbol: str, limit: int = 250) -> list:
        rows = self.conn.execute("""
            SELECT timestamp, open, high, low, close, volume
            FROM market_5m_candles
            WHERE symbol = ?
            ORDER BY timestamp DESC
            LIMIT ?
        """, (symbol, limit)).fetchall()

        # 오래된 순으로 정렬
        rows = list(reversed(rows))
        return [{"timestamp": r[0], "open": r[1], "high": r[2], "low": r[3], "close": r[4], "volume": r[5]} for r in rows]

    def get_current_price(self, symbol: str) -> float:
        row = self.conn.execute("""
            SELECT close FROM market_5m_candles WHERE symbol = ? ORDER BY timestamp DESC LIMIT 1
        """, (symbol,)).fetchone()
        return row[0] if row else 0.0

    def get_stats(self) -> dict:
        rows = self.conn.execute("""
            SELECT symbol, COUNT(*), MIN(timestamp), MAX(timestamp)
            FROM market_5m_candles GROUP BY symbol
        """).fetchall()

        stats = {}
        for row in rows:
            stats[row[0]] = {
                "count": row[1],
                "start": datetime.fromtimestamp(row[2], tz=KST).strftime("%Y-%m-%d %H:%M"),
                "end": datetime.fromtimestamp(row[3], tz=KST).strftime("%Y-%m-%d %H:%M"),
            }
        return stats


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="HADES 5분봉 데이터 수집기")
    parser.add_argument("--symbols", nargs="+", default=DEFAULT_SYMBOLS)
    parser.add_argument("--days", type=int, default=DEFAULT_DAYS)
    parser.add_argument("--live", action="store_true")
    parser.add_argument("--db", type=str, default="bigvolver.db")
    parser.add_argument("--stats", action="store_true")
    args = parser.parse_args()

    collector = Hades5mCollector(args.db)
    try:
        if args.stats:
            stats = collector.get_stats()
            print(json.dumps(stats, indent=2, ensure_ascii=False))
        elif args.live:
            collector.collect_live(args.symbols)
        else:
            print(f"[HADES 5m] Backfilling {args.symbols} for {args.days} days...")
            for symbol in args.symbols:
                collector.backfill(symbol, args.days)
            print(f"\n📊 5m Collection Summary:")
            stats = collector.get_stats()
            for sym, info in stats.items():
                print(f"  {sym}: {info['count']} candles ({info['start']} ~ {info['end']})")
    finally:
        collector.close()
