#!/usr/bin/env bash
# ============================================================================
# Robin Trading Platform — Secret Key Rotation
# Generates a new AES-GCM master key and encrypts/re-encrypts API secrets.
# ============================================================================
set -euo pipefail

VAULT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/.secrets"
MASTER_KEY_FILE="$VAULT_DIR/master.key"
ENV_FILE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/.env"

mkdir -p "$VAULT_DIR"
chmod 700 "$VAULT_DIR"

echo "Robin Security — Key Rotation Utility"
echo "====================================="

if [ -f "$MASTER_KEY_FILE" ]; then
    echo "[INFO] Existing master key found. Generating new key and re-encrypting vault..."
    # Generate new key to a temp file
    openssl rand -base64 32 > "$MASTER_KEY_FILE.new"
    chmod 600 "$MASTER_KEY_FILE.new"
    
    # Run the Python vault utility to re-encrypt
    export ROBIN_MASTER_KEY_OLD="$(cat "$MASTER_KEY_FILE")"
    export ROBIN_MASTER_KEY="$(cat "$MASTER_KEY_FILE.new")"
    
    python3 services/ai-agent/secret_vault.py --rotate
    
    mv "$MASTER_KEY_FILE.new" "$MASTER_KEY_FILE"
    echo "✅ Vault re-encrypted successfully."
else
    echo "[INFO] No existing master key. Initializing new vault..."
    openssl rand -base64 32 > "$MASTER_KEY_FILE"
    chmod 600 "$MASTER_KEY_FILE"
    
    export ROBIN_MASTER_KEY="$(cat "$MASTER_KEY_FILE")"
    
    # Migrate from .env if it exists
    if [ -f "$ENV_FILE" ]; then
        echo "Migrating secrets from .env..."
        python3 services/ai-agent/secret_vault.py --migrate-env "$ENV_FILE"
    else
        python3 services/ai-agent/secret_vault.py --init
    fi
    echo "✅ Vault initialized."
fi

echo "====================================="
echo "Master key stored in: $MASTER_KEY_FILE"
echo "IMPORTANT: Back up this key securely. Without it, your API secrets are lost."
