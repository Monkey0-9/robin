import json
import csv
import os
from datetime import datetime
import uuid
import sqlite3

def generate_cat_report(db_file, output_file):
    if not os.path.exists(db_file):
        print(f"No database file found at {db_file}")
        return

    orders = []
    try:
        conn = sqlite3.connect(db_file)
        cursor = conn.cursor()
        
        # We look for OrderReceived events in the audit log
        cursor.execute("SELECT data FROM audit_log WHERE event = 'OrderReceived'")
        for row in cursor.fetchall():
            try:
                order_data = json.loads(row[0])
                orders.append(order_data)
            except json.JSONDecodeError:
                continue
    except Exception as e:
        print(f"Error reading from SQLite database: {e}")
        return
    finally:
        if 'conn' in locals():
            conn.close()

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
    # Real system reads from the central SQLite database (robin.db)
    db_file = os.path.join(os.path.dirname(__file__), "..", "gateway", "robin.db")
    output_file = f"cat_report_{datetime.now().strftime('%Y%m%d')}.csv"
    
    generate_cat_report(db_file, output_file)
