"""Binance API test - correct endpoints"""
import time, hmac, hashlib, requests, json

API_KEY = "oOgh3xzZFpdYnllKgJTN0CYFPaUmqq2OdXXV8tk3JT78sIiYI5yfgEn1O4TMtxg1"
SECRET = "7xe9F1twOzjOwDAPsUVVRWPJ53LnKOLElSKBkopwmCaUqgOgpyY5IUrqRBi4sCyg"
BASE = "https://api.binance.com"

def sign(params):
    qs = "&".join(f"{k}={v}" for k, v in sorted(params.items()))
    return hmac.new(SECRET.encode(), qs.encode(), hashlib.sha256).hexdigest()

def sapi_get(path, label, extra=None):
    params = {"timestamp": str(int(time.time() * 1000))}
    if extra: params.update(extra)
    params["signature"] = sign(params)
    headers = {"X-MBX-APIKEY": API_KEY}
    try:
        r = requests.get(BASE + path, params=params, headers=headers, timeout=15)
        print(f"[{r.status_code}] {label}")
        if r.text:
            try:
                d = r.json()
                if isinstance(d, dict):
                    for k, v in list(d.items())[:3]:
                        if isinstance(v, (str, int, float, bool)):
                            print(f"  {k}: {v}")
                        elif isinstance(v, list):
                            print(f"  {k}: [{len(v)} items]")
                            for item in d[:5]:
                                if isinstance(item, dict) and item.get("asset") == "USDT":
                                    print(f"  USDT: free={item.get('free')}, locked={item.get('locked')}")
                elif isinstance(d, list):
                    print(f"  [{len(d)} items]")
                    for item in d:
                        if isinstance(item, dict) and item.get("asset") == "USDT":
                            print(f"  USDT: free={item.get('free')}, locked={item.get('locked')}")
            except: pass
        else:
            print(f"  (empty response)")
    except Exception as e:
        print(f"[ERR] {label}: {e}")

print("=== Binance API Full Test ===")

# Account (works)
sapi_get("/api/v3/account", "Account")

# Balance - try sapi endpoint
sapi_get("/sapi/v1/capital/config/getall", "Capital Config")
print()

# System status (public)
try:
    r = requests.get(BASE + "/api/v3/systemStatus", timeout=10)
    print(f"[{r.status_code}] System Status: {r.text}")
except Exception as e:
    print(f"[ERR] System: {e}")
print()

# Test websocket endpoint for data feed availability
sapi_get("/api/v3/ticker/price", "BTC Price (public, no auth)", {"symbol": "BTCUSDT"})
