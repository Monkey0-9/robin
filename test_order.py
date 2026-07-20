import json
import jwt
import time
import os
import requests
import warnings

# Suppress insecure request warnings if using self-signed certs
requests.packages.urllib3.disable_warnings(requests.packages.urllib3.exceptions.InsecureRequestWarning)

key_path = "config/keys/private.pem"
if not os.path.exists(key_path):
    raise ValueError(f"RSA Private key not found at {key_path}")

with open(key_path, "rb") as f:
    private_key = f.read()

token = jwt.encode({
    "aud": "robin-services",
    "exp": int(time.time()) + 3600,
    "iss": "robin-gateway",
    "role": "trader"
}, private_key, algorithm="RS256")

port = os.environ.get("ORCH_PORT", "8080")
url = f"https://localhost:{port}/order"
order_data = {
    "symbol": "BTC/USD",
    "side": "BUY",
    "price": 64000.0,
    "qty": 0.1,
    "order_type": "LIMIT",
    "cl_ord_id": "client-test-alpaca-1"
}
headers = {
    "Authorization": f"Bearer {token}",
    "Content-Type": "application/json"
}

# Requires mTLS certs to be generated via setup_mtls.sh
cert_paths = ('config/certs/client.crt', 'config/certs/client.key')
ca_path = 'config/certs/ca.crt'

try:
    response = requests.post(url, headers=headers, json=order_data, cert=cert_paths, verify=False)
    print(f"Status Code: {response.status_code}")
    print(f"Response: {response.text}")
except requests.exceptions.SSLError as e:
    print(f"SSL/mTLS Error: {e}")
except Exception as e:
    print("Error:", e)
