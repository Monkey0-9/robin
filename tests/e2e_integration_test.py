import requests
import time
import logging

logging.basicConfig(level=logging.INFO)

GATEWAY_URL = "http://localhost:8080"
RISK_WS = "ws://localhost:9092"
AI_AGENT_WS = "ws://localhost:8000/ws/signals"
FRONTEND_URL = "http://localhost:3000"

def test_full_pipeline():
    logging.info("Starting End-to-End Pipeline Verification")

    # 1. Check Gateway Health
    try:
        resp = requests.get(f"{GATEWAY_URL}/health")
        if resp.status_code == 200:
            logging.info("Gateway is UP.")
        else:
            logging.warning("Gateway returned non-200 status.")
    except Exception as e:
        logging.error(f"Gateway connection failed: {e}")

    # 2. Submit Order
    logging.info("Submitting test order through Gateway...")
    order_payload = {
        "instrument": "BTC-USD",
        "side": "buy",
        "type": "limit",
        "price": 60000.0,
        "quantity": 1.0,
        "client_order_id": f"test_{int(time.time())}"
    }
    
    try:
        headers = {"Authorization": "Bearer TEST_TOKEN"}
        resp = requests.post(f"{GATEWAY_URL}/api/v1/orders", json=order_payload, headers=headers)
        if resp.status_code in [200, 201]:
            logging.info("Order submitted successfully.")
            order_data = resp.json()
            logging.info(f"Order ID: {order_data.get('id', 'unknown')}")
        else:
            logging.warning(f"Order submission failed: {resp.status_code} - {resp.text}")
    except Exception as e:
        logging.error(f"Order submission error: {e}")

    # 3. Check AI Agent
    try:
        resp = requests.get(f"http://localhost:8000/")
        if resp.status_code == 200:
            logging.info("AI Agent API is UP.")
    except Exception as e:
        logging.error(f"AI Agent connection failed: {e}")

    logging.info("E2E Pipeline Integration Script Complete.")

if __name__ == "__main__":
    test_full_pipeline()
