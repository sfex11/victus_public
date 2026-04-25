"""
BigVolver Data Collector — Fetches historical + live data from Binance.

Populates SQLite database with:
- 1h OHLCV candles (market_1h_candles)
- Funding rates (market_funding_rate)

Usage:
    # Initial backfill (90 days)
    python data_collector.py --symbols BTCUSDT ETHUSDT --days 90 --db bigvolver.db

    # Continuous live collection
    python data_collector.py --symbols BTCUSDT ETHUSDT --live --db bigvolver.db

    # One-shot for end-to-end test
    python data_collector.py --symbols BTCUSDT --days 30 --db bigvolver.db
"""

import argparse
import json
import os
import sqlite3
import sys
import time
from datetime import datetime, timedelta, timezone
from pathlib import Path

import numpy as np

# Binance API base (public, no API key needed for klines/funding)
BINANCE_BASE = "https://fapi.binance.com"

KST = timezone(timedelta(hours=9))

DEFAULT_SYMBOLS = ["BTCUSDT", "ETHUSDT", "SOLUSDT", "DOGEUSDT"]
DEFAULT_DAYS = 90


class BinanceCollector:
    """Fetches market data from Binance Futures API."""

    def __init__(self, db_path: str = "bigvolver.db"):
        self.db_path = db_path
        self.conn = None
        self._init_db()

    def _init_db(self):
        """Initialize SQLite database with required tables."""
        self.conn = sqlite3.connect(self.db_path)
        self.conn.execute("PRAGMA journal_mode=WAL")
        self.conn.execute("""
            CREATE TABLE IF NOT EXISTS market_1h_candles (
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
        self.conn.execute("""
            CREATE TABLE IF NOT EXISTS market_funding_rate (
                symbol TEXT NOT NULL,
                funding_time INTEGER NOT NULL,
                funding_rate REAL NOT NULL,
                PRIMARY KEY (symbol, funding_time)
            )
        """)
        self.conn.commit()

    def close(self):
        if self.conn:
            self.conn.close()

    # ----------------------------------------------------------
    # Klines (OHLCV)
    # ----------------------------------------------------------

    def fetch_klines(
        self,
        symbol: str,
        interval: str = "1h",
        start_time: int = None,
        end_time: int = None,
        limit: int = 1500,
    ) -> list:
        """
        Fetch klines from Binance Futures.

        Returns list of dicts: {symbol, timestamp, open, high, low, close, volume}
        """
        url = f"{BINANCE_BASE}/fapi/v1/klines"
        params = {
            "symbol": symbol,
            "interval": interval,
            "limit": limit,
        }
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
            print(f"[Collector] Failed to fetch {symbol} klines: {e}")
            return []

        candles = []
        for k in data:
            candles.append({
                "symbol": symbol,
                "timestamp": int(k[0] // 1000),  # ms → s
                "open": float(k[1]),
                "high": float(k[2]),
                "low": float(k[3]),
                "close": float(k[4]),
                "volume": float(k[5]),
            })

        return candles

    def backfill_klines(self, symbol: str, days: int = 90):
        """Fetch all historical klines for the past N days."""
        end_time = int(datetime.now(timezone.utc).timestamp() * 1000)
        start_time = int((datetime.now(timezone.utc) - timedelta(days=days)).timestamp() * 1000)

        total_inserted = 0
        current_start = start_time

        while current_start < end_time:
            candles = self.fetch_klines(
                symbol=symbol,
                start_time=current_start,
                end_time=end_time,
                limit=1500,
            )

            if not candles:
                break

            # Insert into DB (upsert)
            inserted = self._upsert_candles(candles)
            total_inserted += inserted

            # Move start time past last candle
            last_ts = candles[-1]["timestamp"] * 1000 + 1
            if last_ts <= current_start:
                break
            current_start = last_ts

            # Rate limit
            time.sleep(0.2)

            # Progress
            progress = min(100, (current_start - start_time) / (end_time - start_time) * 100)
            pct = f"{progress:.0f}%"
        sys.stdout.write(f"\r  {symbol}: {pct} ({total_inserted} candles)")
        sys.stdout.flush()

        sys.stdout.write(f"\r  {symbol}: OK {total_inserted} candles backfilled          \n")
        sys.stdout.flush()
        return total_inserted

    def fetch_latest_kline(self, symbol: str) -> dict:
        """Fetch the latest single kline."""
        candles = self.fetch_klines(symbol, limit=1)
        return candles[0] if candles else None

    # ----------------------------------------------------------
    # Funding Rates
    # ----------------------------------------------------------

    def fetch_funding_rates(
        self,
        symbol: str,
        start_time: int = None,
        end_time: int = None,
        limit: int = 1000,
    ) -> list:
        """Fetch funding rate history from Binance Futures."""
        url = f"{BINANCE_BASE}/fapi/v1/fundingRate"
        params = {"symbol": symbol, "limit": limit}
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
            print(f"[Collector] Failed to fetch {symbol} funding: {e}")
            return []

        rates = []
        for r in data:
            rates.append({
                "symbol": symbol,
                "funding_time": int(r["fundingTime"] // 1000),
                "funding_rate": float(r["fundingRate"]),
            })

        return rates

    def backfill_funding_rates(self, symbol: str, days: int = 90):
        """Fetch all historical funding rates for the past N days."""
        end_time = int(datetime.now(timezone.utc).timestamp() * 1000)
        start_time = int((datetime.now(timezone.utc) - timedelta(days=days)).timestamp() * 1000)

        total_inserted = 0
        current_start = start_time

        while current_start < end_time:
            rates = self.fetch_funding_rates(
                symbol=symbol,
                start_time=current_start,
                end_time=end_time,
                limit=1000,
            )

            if not rates:
                break

            inserted = self._upsert_funding_rates(rates)
            total_inserted += inserted

            last_ts = rates[-1]["funding_time"] * 1000 + 1
            if last_ts <= current_start:
                break
            current_start = last_ts

            time.sleep(0.2)

        print(f"  {symbol}: OK {total_inserted} funding rates backfilled")
        return total_inserted

    # ----------------------------------------------------------
    # Live collection
    # ----------------------------------------------------------

    def collect_live(self, symbols: list, interval_seconds: int = 300):
        """Continuously collect latest data (runs until interrupted)."""
        print(f"[Collector] Live collection started. Symbols: {symbols}")
        print(f"[Collector] Polling every {interval_seconds}s. Press Ctrl+C to stop.")

        while True:
            try:
                for symbol in symbols:
                    # Latest kline
                    kline = self.fetch_latest_kline(symbol)
                    if kline:
                        self._upsert_candles([kline])

                    # Latest funding rate
                    rates = self.fetch_funding_rates(symbol, limit=1)
                    if rates:
                        self._upsert_funding_rates(rates)

                now = datetime.now(KST).strftime("%H:%M:%S")
                sys.stdout.write(f"\r  [{now}] Collected latest data for {len(symbols)} symbols")
                sys.stdout.flush()
                time.sleep(interval_seconds)

            except KeyboardInterrupt:
                print("\n[Collector] Live collection stopped.")
                break

    # ----------------------------------------------------------
    # DB helpers
    # ----------------------------------------------------------

    def _upsert_candles(self, candles: list) -> int:
        """Insert or update candles (UPSERT)."""
        inserted = 0
        for c in candles:
            try:
                self.conn.execute("""
                    INSERT OR REPLACE INTO market_1h_candles
                    (symbol, timestamp, open, high, low, close, volume)
                    VALUES (?, ?, ?, ?, ?, ?, ?)
                """, (c["symbol"], c["timestamp"], c["open"], c["high"],
                      c["low"], c["close"], c["volume"]))
                inserted += 1
            except Exception as e:
                print(f"\n  DB error: {e}")
        self.conn.commit()
        return inserted

    def _upsert_funding_rates(self, rates: list) -> int:
        """Insert or update funding rates."""
        inserted = 0
        for r in rates:
            try:
                self.conn.execute("""
                    INSERT OR REPLACE INTO market_funding_rate
                    (symbol, funding_time, funding_rate)
                    VALUES (?, ?, ?)
                """, (r["symbol"], r["funding_time"], r["funding_rate"]))
                inserted += 1
            except Exception as e:
                print(f"\n  DB error: {e}")
        self.conn.commit()
        return inserted

    def get_stats(self) -> dict:
        """Return data collection statistics."""
        stats = {}

        # Candle counts per symbol
        rows = self.conn.execute("""
            SELECT symbol, COUNT(*), MIN(timestamp), MAX(timestamp)
            FROM market_1h_candles
            GROUP BY symbol
        """).fetchall()

        stats["candles"] = {}
        for row in rows:
            stats["candles"][row[0]] = {
                "count": row[1],
                "start": datetime.fromtimestamp(row[2], tz=KST).strftime("%Y-%m-%d %H:%M"),
                "end": datetime.fromtimestamp(row[3], tz=KST).strftime("%Y-%m-%d %H:%M"),
            }

        # Funding rate counts
        rows = self.conn.execute("""
            SELECT symbol, COUNT(*), MIN(funding_time), MAX(funding_time)
            FROM market_funding_rate
            GROUP BY symbol
        """).fetchall()

        stats["funding_rates"] = {}
        for row in rows:
            stats["funding_rates"][row[0]] = {
                "count": row[1],
                "start": datetime.fromtimestamp(row[2], tz=KST).strftime("%Y-%m-%d %H:%M"),
                "end": datetime.fromtimestamp(row[3], tz=KST).strftime("%Y-%m-%d %H:%M"),
            }

        return stats

    # ----------------------------------------------------------
    # Export to CSV (for ML/DRL training)
    # ----------------------------------------------------------

    def export_to_csv(self, output_path: str, symbol: str = None) -> str:
        """
        Export candles to CSV for ML pipeline consumption.

        If symbol is None, exports all symbols.
        Returns the output file path.
        """
        if symbol:
            query = """
                SELECT symbol, timestamp, open, high, low, close, volume
                FROM market_1h_candles
                WHERE symbol = ?
                ORDER BY timestamp ASC
            """
            df = self.conn.execute(query, (symbol,)).fetchall()
        else:
            query = """
                SELECT symbol, timestamp, open, high, low, close, volume
                FROM market_1h_candles
                ORDER BY symbol, timestamp ASC
            """
            df = self.conn.execute(query).fetchall()

        path = Path(output_path)
        path.parent.mkdir(parents=True, exist_ok=True)

        with open(path, "w") as f:
            f.write("symbol,timestamp,open,high,low,close,volume\n")
            for row in df:
                f.write(f"{row[0]},{row[1]},{row[2]},{row[3]},{row[4]},{row[5]},{row[6]}\n")

        print(f"[Collector] Exported {len(df)} rows to {path}")
        return str(path)


# ============================================================
# Main
# ============================================================

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="BigVolver Data Collector")
    parser.add_argument("--symbols", nargs="+", default=DEFAULT_SYMBOLS, help="Trading symbols")
    parser.add_argument("--days", type=int, default=DEFAULT_DAYS, help="Historical days to backfill")
    parser.add_argument("--live", action="store_true", help="Run continuous live collection")
    parser.add_argument("--db", type=str, default="bigvolver.db", help="SQLite database path")
    parser.add_argument("--export", type=str, help="Export candles to CSV after collection")
    parser.add_argument("--stats", action="store_true", help="Print collection stats")
    args = parser.parse_args()

    collector = BinanceCollector(args.db)

    try:
        if args.stats:
            stats = collector.get_stats()
            print(json.dumps(stats, indent=2, ensure_ascii=False))
        elif args.live:
            collector.collect_live(args.symbols)
        else:
            print(f"[Collector] Backfilling {args.symbols} for {args.days} days...")
            for symbol in args.symbols:
                print(f"\n📊 {symbol} — klines:")
                collector.backfill_klines(symbol, args.days)

                print(f"  {symbol} — funding rates:")
                collector.backfill_funding_rates(symbol, args.days)

            if args.export:
                collector.export_to_csv(args.export)

            # Print stats
            print(f"\n📊 Collection Summary:")
            stats = collector.get_stats()
            for sym, info in stats.get("candles", {}).items():
                print(f"  {sym}: {info['count']} candles ({info['start']} ~ {info['end']})")
            for sym, info in stats.get("funding_rates", {}).items():
                print(f"  {sym}: {info['count']} funding rates")

    finally:
        collector.close()
