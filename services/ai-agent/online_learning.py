import logging
import schedule
import time
import argparse
from datetime import datetime
from model_trainer import AITrainer
from data_engine import DataEngine

logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")
logger = logging.getLogger("online_learning")

def retrain_models():
    """
    Weekly cron job to fetch new data and retrain ML models.
    """
    logger.info("Starting weekly online retraining cycle...")
    
    # 1. Update datasets
    engine = DataEngine()
    try:
        # Assuming data_engine has an update method, otherwise we just fetch everything
        # In a real system, we would just fetch the delta.
        engine.fetch_all(days_back=5000) # Fetch full history to ensure complete features
    except Exception as e:
        logger.error(f"Failed to fetch new data: {e}")
        return

    # 2. Retrain all models
    try:
        trainer = AITrainer()
        trainer.train_all()
        trainer.train_signal_classifier()
        logger.info("Online retraining cycle completed successfully.")
    except Exception as e:
        logger.error(f"Failed to retrain models: {e}")

if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--run-now", action="store_true", help="Run once immediately")
    args = parser.parse_args()

    if args.run_now:
        retrain_models()
    else:
        logger.info("Starting online learning cron service...")
        # Schedule for Sunday at 00:00
        schedule.every().sunday.at("00:00").do(retrain_models)
        
        while True:
            schedule.run_pending()
            time.sleep(60)
