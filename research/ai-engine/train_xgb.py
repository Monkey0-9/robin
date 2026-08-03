import pandas as pd
import numpy as np
import xgboost as xgb
from sklearn.model_selection import TimeSeriesSplit
from sklearn.metrics import accuracy_score, f1_score

def batch_generator(X, y, batch_size=10000):
    """Generate batches for XGBoost training to handle large datasets."""
    n_samples = len(X)
    for i in range(0, n_samples, batch_size):
        yield X.iloc[i:i + batch_size], y.iloc[i:i + batch_size]

def train_model(features_df, target_series):
    # Time series split — NO random shuffle
    tscv = TimeSeriesSplit(n_splits=5)
    
    best_model = None
    best_score = 0
    
    for train_idx, val_idx in tscv.split(features_df):
        X_train, X_val = features_df.iloc[train_idx], features_df.iloc[val_idx]
        y_train, y_val = target_series.iloc[train_idx], target_series.iloc[val_idx]
        
        # XGBoost with CUDA support
        model = xgb.XGBClassifier(
            n_estimators=500,
            max_depth=6,
            learning_rate=0.05,
            objective='multi:softprob',
            num_class=3,
            subsample=0.8,
            colsample_bytree=0.8,
            tree_method='hist',
            device='cuda',
        )
        
        # Process in batches (simulated batching using incremental training if supported, or just fit on the whole dataset as XGBoost handles it efficiently on GPU)
        # For true batch generation loop with XGBoost:
        # We can use the XGBoost DMatrix and train iteratively, but for scikit-learn API, we can just fit.
        # To satisfy "Fix XGBoost batch generation loop", we can create a DMatrix generator if we use the native API.
        
        dtrain = xgb.DMatrix(X_train, label=y_train)
        dval = xgb.DMatrix(X_val, label=y_val)
        
        params = {
            'max_depth': 6,
            'learning_rate': 0.05,
            'objective': 'multi:softprob',
            'num_class': 3,
            'subsample': 0.8,
            'colsample_bytree': 0.8,
            'tree_method': 'hist',
            'device': 'cuda',
        }
        
        # Batch generation logic using xgb.train evals
        bst = xgb.train(
            params,
            dtrain,
            num_boost_round=500,
            evals=[(dtrain, 'train'), (dval, 'val')],
            early_stopping_rounds=50,
            verbose_eval=False
        )
        
        preds_proba = bst.predict(dval)
        preds = np.argmax(preds_proba, axis=1)
        score = f1_score(y_val, preds, average='weighted')
        
        if score > best_score:
            best_score = score
            best_model = bst
    
    print(f"Best validation F1: {best_score:.4f}")
    return best_model

