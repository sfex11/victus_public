"""Binance API Full Test - with debug"""
import time, hmac, hashlib, requests, json

API_KEY = "oOgh3xzZFpdYnllKgJTN0CYFPaUmqq2OdXXV8tk3JT78sIiYI5yfgEn1O4TMtxg1"
SECRET = "7xe9F1twOzjOwDAPsUVVRWPJ53LnKOLElSKBkopwmCaUqgOgpyY5IUrqRBi4sCyg"
BASE = "https://api.binance.com"

def sign(params):
    qs = "&".join(f"{k}={v}" for k, v in sorted(params.items()))
    return hmac.new(SECRET.encode(), qs.encode(), hashlib.sha256).hexdigest()

def signed_get(path, label, extra=None):
    params = {"timestamp": str(int(time.time() * 1000))}
    if extra:
        params.update(extra)
    params["signature"] = sign(params)
    headers = {"X-MBX-APIKEY": API_KEY}
    try:
        r = requests.get(BASE + path, params=params, headers=headers, timeout=15)
        print(f"[{r.status_code}] {label}")
        print(f"  Raw: {r.text[:500]}")
        return r.json()
    except Exception as e:
        print(f"[ERR] {label}: {e}")
        return None

# Test all private endpoints individually
print("=== Binance Private API Test ===")
signed_get("/api/v3/balance", "Balance")
print()
signed_get("/api/v3/system/status", "System Status")
print()
signed_get("/api/v3/exchangeInfo", "Exchange Info (basic)")
