import urllib.request
import json

url = "http://localhost:18080/order"
data = json.dumps({"symbol":"BTC/USD","side":"BUY","price":64000.0,"qty":1.0,"order_type":"LIMIT","cl_ord_id":"client-test"}).encode("utf-8")
headers = {"Authorization": "Bearer smoke-test-secret", "Content-Type": "application/json"}
req = urllib.request.Request(url, data=data, headers=headers)

try:
    with urllib.request.urlopen(req, timeout=5) as response:
        print("Status:", response.status)
        print("Response:", response.read().decode())
except Exception as e:
    print("Error:", e)
