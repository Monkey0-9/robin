import json
import csv
import os
from datetime import datetime
import uuid

def generate_cat_report(orders_file, output_file):
    if not os.path.exists(orders_file):
        print(f"No orders file found at {orders_file}")
        return

    with open(orders_file, 'r') as f:
        orders = json.load(f)

    # FINRA CAT Format (Consolidated Audit Trail)
    # 2a MENO (New Order Event)
    # actionType, errorROEID, firmROEID, type, brokerRoEID, CATReporterCRD, orderKeyDate, orderID, symbol, eventTimestamp, manualFlag, deptType, price, qty, side, timeInForce, tradingSession, accountHolderType, handlingInstructions

    with open(output_file, 'w', newline='') as csvfile:
        fieldnames = ['actionType', 'firmROEID', 'type', 'CATReporterCRD', 'orderKeyDate', 'orderID', 'symbol', 'eventTimestamp', 'manualFlag', 'price', 'qty', 'side']
        writer = csv.DictWriter(csvfile, fieldnames=fieldnames)

        writer.writeheader()
        
        for order in orders:
            event_time = order.get('timestamp', datetime.utcnow().isoformat())
            key_date = event_time.split('T')[0]
            
            writer.writerow({
                'actionType': 'NEW',
                'firmROEID': str(uuid.uuid4()),
                'type': 'MENO',
                'CATReporterCRD': '99999',
                'orderKeyDate': key_date,
                'orderID': order.get('id', 'UNKNOWN'),
                'symbol': order.get('symbol', 'UNKNOWN'),
                'eventTimestamp': event_time,
                'manualFlag': 'false',
                'price': order.get('price', 0.0),
                'qty': order.get('qty', 0.0),
                'side': order.get('side', 'BUY')
            })
            
    print(f"Successfully generated FINRA CAT report: {output_file}")

if __name__ == "__main__":
    # In a real system, this reads from the central database
    # For prototype, we can read a mock orders.json
    orders_file = "mock_orders.json"
    output_file = f"cat_report_{datetime.now().strftime('%Y%m%d')}.csv"
    
    if not os.path.exists(orders_file):
        with open(orders_file, 'w') as f:
            json.dump([
                {"id": "ORD123", "symbol": "BTC-USD", "price": 50000.0, "qty": 1.5, "side": "BUY", "timestamp": datetime.utcnow().isoformat()}
            ], f)
            
    generate_cat_report(orders_file, output_file)
