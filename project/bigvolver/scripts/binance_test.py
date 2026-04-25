"""Binance API Key Test - Public + Auth endpoints"""
import time
import hmac
import hashlib
import requests
import json

API_KEY = "oOgh3xzZFpdYnllKgJTN0CYFPaUmqq2OdXXV8tk3JT78sIiYI5yfgEn1O4TMtxg1"
SECRET = "7xe9F1twOzjOwDAPsUVVRWPJ53LnKOLElSKBkopwmCaUqgOgpyY5IUrqRBi4sCyg"
BASE = "https://api.binance.com"

def sign(params):
    qs = "&".join(f"{k}={v}" for k, v in sorted(params.items()))
    return hmac.new(SECRET.encode(), qs.encode(), hashlib.sha256).hexdigest()

def public_get(path, label):
    try:
        r = requests.get(BASE + path, timeout=10)
        print(f"[OK] {label}: {r.status_code} {json.dumps(r.json(), ensure_ascii=False)[:200]}")
    except Exception as e:
        print(f"[FAIL] {label}: {e}")

def private_get(path, label, params=None):
    if params is None:
        params = {}
    params["timestamp"] = str(int(time.time() * 1000))
    params["signature"] = sign(params)
    headers = {"X-MBX-APIKEY": API_KEY}
    try:
        r = requests.get(BASE + path, params=params, headers=headers, timeout=15)
        data = r.json()
        if r.status_code == 200:
            print(f"[OK] {label}: {r.status_code}")
            if isinstance(data, dict):
                for k, v in list(data.items())[:5]:
                    if isinstance(v, (int, float, str, bool)):
                        print(f"   {k}: {v}")
                    elif isinstance(v, list):
                        print(f"   {k}: [{len(v)} items]")
                    else:
                        print(f"   {k}: {str(v)[:100]}")
            elif isinstance(data, list):
                print(f"   {len(data)} items")
                for item in data[:3]:
                    if isinstance(item, dict) and item.get("asset") == "USDT":
                        print(f"   USDT free={item.get('free')}, locked={item.get('locked')}")
                    elif isinstance(item, dict):
                        print(f"   {item.get('asset','?')}: free={item.get('free','?')}")
        else:
            print(f"[FAIL] {label}: {r.status_code} {json.dumps(data, ensure_ascii=False)}")
    except Exception as e:
        print(f"[FAIL] {label}: {e}")

print("=" * 50)
print("Binance API Key Test")
print("=" * 50)

print()
print("--- Public Endpoints ---")
public_get("/api/v3/time", "Server Time")
public_get("/api/v3/ticker/price?symbol=BTCUSDT", "BTC/USDT Price")
public_get("/api/v3/ticker/price?symbol=ETHUSDT", "ETH/USDT Price")
public_get("/api/v3/ticker/24hr?symbol=BTCUSDT", "BTC 24h")

print()
print("--- Private Endpoints ---")
private_get("/api/v3/account", "Account Info")
private_get("/api/v3/balance", "Balance")

print()
print("--- IP Restriction ---")
private_get("/api/v3/system/status", "System Status")
