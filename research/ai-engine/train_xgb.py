import pandas as pd
import numpy as np
from lightgbm import LGBMClassifier
from sklearn.model_selection import TimeSeriesSplit
from sklearn.metrics import accuracy_score, f1_score

def train_model(features_df, target_series):
    # Time series split — NO random shuffle
    tscv = TimeSeriesSplit(n_splits=5)
    
    best_model = None
    best_score = 0
    
    for train_idx, val_idx in tscv.split(features_df):
        X_train, X_val = features_df.iloc[train_idx], features_df.iloc[val_idx]
        y_train, y_val = target_series.iloc[train_idx], target_series.iloc[val_idx]
        
        model = LGBMClassifier(
            n_estimators=500,
            max_depth=6,
            learning_rate=0.05,
            objective='multiclass',
            num_class=3,
            feature_fraction=0.8,
            bagging_fraction=0.8,
        )
        model.fit(X_train, y_train)
        
        preds = model.predict(X_val)
        score = f1_score(y_val, preds, average='weighted')
        
        if score > best_score:
            best_score = score
            best_model = model
    
    print(f"Best validation F1: {best_score:.4f}")
    return best_model
