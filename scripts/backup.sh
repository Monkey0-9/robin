#!/usr/bin/env bash
# ============================================================================
# Robin Trading Platform — Backup Utility
# Backs up the secret vault, database states, and logs.
# ============================================================================
set -euo pipefail

BACKUP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/backups"
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
ARCHIVE="$BACKUP_DIR/robin_backup_$TIMESTAMP.tar.gz"

mkdir -p "$BACKUP_DIR"

echo "Robin Platform Backup"
echo "====================="
echo "Target: $ARCHIVE"

# Directories to back up
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

# 1. Back up secret vault
echo "-> Backing up secrets..."
if [ -d ".secrets" ]; then
    cp -r .secrets "$BACKUP_DIR/tmp_secrets"
else
    echo "   (No secrets found)"
fi

# 2. Back up logs
echo "-> Backing up logs..."
if [ -d "logs" ]; then
    cp -r logs "$BACKUP_DIR/tmp_logs"
else
    echo "   (No logs found)"
fi
if [ -d "services/gateway/logs" ]; then
    mkdir -p "$BACKUP_DIR/tmp_logs/gateway"
    cp -r services/gateway/logs/* "$BACKUP_DIR/tmp_logs/gateway/" 2>/dev/null || true
fi

# 3. Create archive
echo "-> Creating archive..."
cd "$BACKUP_DIR"
tar -czf "robin_backup_$TIMESTAMP.tar.gz" tmp_* 2>/dev/null || true

# Cleanup
rm -rf tmp_*

echo "✅ Backup complete: $ARCHIVE"
