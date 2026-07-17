import urllib.request
import json

import jwt
import time
import os

secret = os.environ.get("ROBIN_GATEWAY_API_TOKEN", "new-secure-dev-secret")
token = jwt.encode({
    "aud": "robin-services",
    "exp": int(time.time()) + 3600,
    "iss": "robin-gateway",
    "role": "trader"
}, secret, algorithm="HS256")

url = "http://localhost:8080/order"
order_data = {
    "symbol": "BTC/USD",
    "side": "BUY",
    "price": 64000.0,
    "qty": 1.0,
    "order_type": "LIMIT",
    "cl_ord_id": "client-test"
}
data = json.dumps(order_data).encode("utf-8")
headers = {
    "Authorization": f"Bearer {token}",
    "Content-Type": "application/json"
}
req = urllib.request.Request(url, data=data, headers=headers)

try:
    with urllib.request.urlopen(req, timeout=5) as response:
        print("Status:", response.status)
        print("Response:", response.read().decode())
except Exception as e:
    print("Error:", e)
