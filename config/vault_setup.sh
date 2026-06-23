#!/usr/bin/env bash
# ============================================================================
# Robin Trading Platform — HashiCorp Vault Setup & Configuration
# ============================================================================
# This script provisions a Vault server for the Robin platform:
#   1. Initializes the Vault server and captures unseal keys + root token.
#   2. Unseals the vault using the generated keys.
#   3. Mounts a KV v2 secrets engine for trading configuration.
#   4. Configures dynamic database credentials for PostgreSQL.
#   5. Sets up AppRole auth for service-to-service authentication.
#   6. Enables the Transit engine for audit log signing.
#
# Prerequisites:
#   - Vault binary installed and in PATH
#   - Vault server running (e.g. vault server -dev or production HA mode)
#   - VAULT_ADDR environment variable set (default: http://127.0.0.1:8200)
#
# Usage:
#   chmod +x vault_setup.sh
#   export VAULT_ADDR=http://127.0.0.1:8200
#   ./vault_setup.sh
# ============================================================================

set -euo pipefail

VAULT_ADDR="${VAULT_ADDR:-http://127.0.0.1:8200}"
VAULT_TOKEN="${VAULT_TOKEN:-}"

# Color helpers
info()  { echo -e "\033[1;34m[INFO]\033[0m $*"; }
ok()    { echo -e "\033[1;32m[OK]\033[0m   $*"; }
warn()  { echo -e "\033[1;33m[WARN]\033[0m  $*"; }
err()   { echo -e "\033[1;31m[ERR]\033[0m   $*"; }

# -------------------------------------------------------------------
# Helper: run a vault CLI command with optional error handling
# -------------------------------------------------------------------
vault_cmd() {
    VAULT_ADDR="$VAULT_ADDR" vault "$@"
}

vault_auth_cmd() {
    VAULT_ADDR="$VAULT_ADDR" VAULT_TOKEN="$VAULT_TOKEN" vault "$@"
}

# ============================================================================
# Step 1 — Initialize Vault
# ============================================================================
init_vault() {
    info "=== Step 1: Initializing Vault server ==="

    # Check if already initialized
    if vault_cmd status -format=json 2>/dev/null | grep -q '"initialized": true'; then
        warn "Vault is already initialized. Skipping init."
        return
    fi

    # Initialize with 5 unseal keys, threshold 3
    INIT_OUT=$(vault_cmd operator init \
        -key-shares=5 \
        -key-threshold=3 \
        -format=json 2>&1)

    echo "$INIT_OUT" > /tmp/vault_init_keys.json

    info "Vault initialized. Unseal keys written to /tmp/vault_init_keys.json"
    info "Store these keys securely! They are NOT recoverable."

    # Extract root token for subsequent commands
    VAULT_TOKEN=$(echo "$INIT_OUT" | grep -o '"root_token": *"[^"]*"' | cut -d'"' -f4)
    if [[ -z "$VAULT_TOKEN" ]]; then
        err "Failed to extract root token from init output."
        exit 1
    fi

    ok "Root token: $VAULT_TOKEN"
    export VAULT_TOKEN
}

# ============================================================================
# Step 2 — Unseal Vault
# ============================================================================
unseal_vault() {
    info "=== Step 2: Unsealing Vault ==="

    if vault_cmd status -format=json 2>/dev/null | grep -q '"sealed": false'; then
        warn "Vault is already unsealed. Skipping."
        return
    fi

    KEY_FILE="${1:-/tmp/vault_init_keys.json}"
    if [[ ! -f "$KEY_FILE" ]]; then
        err "Unseal keys file not found: $KEY_FILE"
        info "Provide the path to the init keys JSON file as the first argument."
        exit 1
    fi

    # Extract the first 3 unseal keys (key_threshold=3)
    for i in 0 1 2; do
        KEY=$(jq -r ".unseal_keys_b64[$i]" "$KEY_FILE")
        if [[ -z "$KEY" || "$KEY" == "null" ]]; then
            err "Failed to extract unseal key index $i from $KEY_FILE"
            exit 1
        fi
        vault_cmd operator unseal "$KEY" >/dev/null 2>&1
        info "Unseal key $((i+1))/3 applied."
    done

    if vault_cmd status -format=json 2>/dev/null | grep -q '"sealed": false'; then
        ok "Vault is now unsealed."
    else
        err "Vault is still sealed after applying 3 keys."
        exit 1
    fi
}

# ============================================================================
# Step 3 — Mount KV v2 Secrets Engine for Trading Config
# ============================================================================
mount_kv_v2() {
    info "=== Step 3: Mounting KV v2 secrets engine ==="

    MOUNT_PATH="robin-config"
    if vault_auth_cmd secrets list -format=json 2>/dev/null | grep -q "\"$MOUNT_PATH/\""; then
        warn "Secrets engine already mounted at $MOUNT_PATH/. Skipping."
        return
    fi

    vault_auth_cmd secrets enable -path="$MOUNT_PATH" -version=2 kv
    ok "KV v2 engine mounted at $MOUNT_PATH/"

    # Seed initial trading configuration values
    info "Seeding initial configuration..."
    vault_auth_cmd kv put "$MOUNT_PATH/trading/limits" \
        position_limit=100000 \
        credit_limit=10000000000 \
        max_orders_per_sec=100 \
        price_collar_bps=500 \
        drawdown_limit=0.10

    vault_auth_cmd kv put "$MOUNT_PATH/trading/ports" \
        orchestrator=8080 \
        execution_health=9091 \
        risk_health=9092 \
        market_data=9093 \
        portfolio=9094 \
        compliance=9095

    vault_auth_cmd kv put "$MOUNT_PATH/trading/multicast" \
        group="233.0.0.1" \
        port=5000

    vault_auth_cmd kv put "$MOUNT_PATH/trading/audit" \
        log_path="/var/log/robin/audit.log" \
        dev_log_path="logs/audit.log"

    ok "Trading configuration seeded."
}

# ============================================================================
# Step 4 — Configure Dynamic Database Credentials (PostgreSQL)
# ============================================================================
configure_database_creds() {
    info "=== Step 4: Configuring dynamic database credentials ==="

    DB_MOUNT="robin-db"
    if vault_auth_cmd secrets list -format=json 2>/dev/null | grep -q "\"$DB_MOUNT/\""; then
        warn "Database secrets engine already mounted at $DB_MOUNT/. Skipping."
        return
    fi

    vault_auth_cmd secrets enable -path="$DB_MOUNT" database

    DB_HOST="${ROBIN_DB_HOST:-127.0.0.1}"
    DB_PORT="${ROBIN_DB_PORT:-5432}"
    DB_NAME="${ROBIN_DB_NAME:-robin}"
    DB_USER="${ROBIN_DB_ADMIN_USER:-vault_admin}"
    DB_PASSWORD="${ROBIN_DB_ADMIN_PASSWORD:-}"

    if [[ -z "$DB_PASSWORD" ]]; then
        warn "ROBIN_DB_ADMIN_PASSWORD not set. Skipping DB configuration."
        warn "Set the environment variable and re-run this step."
        return
    fi

    vault_auth_cmd write "$DB_MOUNT/config/postgresql" \
        plugin_name="postgresql-database-plugin" \
        allowed_roles="robin-app" \
        connection_url="postgresql://{{username}}:{{password}}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=require" \
        username="$DB_USER" \
        password="$DB_PASSWORD"

    vault_auth_cmd write "$DB_MOUNT/roles/robin-app" \
        db_name="postgresql" \
        creation_statements="CREATE USER \"{{name}}\" WITH PASSWORD '{{password}}' VALID UNTIL '{{expiration}}'; GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO \"{{name}}\";" \
        default_ttl="1h" \
        max_ttl="24h"

    ok "Dynamic database credentials configured for role 'robin-app'."
}

# ============================================================================
# Step 5 — Set Up AppRole Auth for Service-to-Service Auth
# ============================================================================
setup_approle() {
    info "=== Step 5: Setting up AppRole authentication ==="

    if vault_auth_cmd auth list -format=json 2>/dev/null | grep -q '"approle/"'; then
        warn "AppRole auth method already enabled. Skipping."
    else
        vault_auth_cmd auth enable approle
        ok "AppRole auth method enabled."
    fi

    # Create a role for the Robin gateway service
    vault_auth_cmd write auth/approle/role/robin-gateway \
        token_policies="robin-gateway-policy" \
        token_ttl="1h" \
        token_max_ttl="24h" \
        bind_secret_id="true" \
        secret_id_num_uses="0" \
        secret_id_ttl="0" \
        token_bound_cidrs="127.0.0.1"

    # Create a policy for the gateway
    vault_auth_cmd policy write robin-gateway-policy -<<EOF
path "robin-config/data/trading/*" {
    capabilities = ["read", "list"]
}

path "robin-db/creds/robin-app" {
    capabilities = ["read"]
}

path "transit/sign/audit-log" {
    capabilities = ["create", "update"]
}
EOF

    local ROLE_ID
    ROLE_ID=$(vault_auth_cmd read -field=role_id auth/approle/role/robin-gateway/role-id)
    local SECRET_ID
    SECRET_ID=$(vault_auth_cmd write -f -field=secret_id auth/approle/role/robin-gateway/secret-id)

    ok "AppRole 'robin-gateway' created."
    info "Role ID:  $ROLE_ID"
    info "Secret ID: $SECRET_ID"
    info "Store the Secret ID securely (e.g., in a Kubernetes secret or env var)."
}

# ============================================================================
# Step 6 — Enable Transit Engine for Audit Log Signing
# ============================================================================
setup_transit() {
    info "=== Step 6: Enabling Transit engine for audit log signing ==="

    if vault_auth_cmd secrets list -format=json 2>/dev/null | grep -q '"transit/"'; then
        warn "Transit engine already enabled. Skipping."
    else
        vault_auth_cmd secrets enable transit
        ok "Transit engine enabled."
    fi

    # Create a signing key for audit logs (Ed25519 for small signatures)
    if vault_auth_cmd read transit/keys/audit-log -format=json >/dev/null 2>&1; then
        warn "Transit key 'audit-log' already exists. Skipping creation."
    else
        vault_auth_cmd write -f transit/keys/audit-log \
            type="ed25519" \
            deletion_allowed="false" \
            exportable="false"
        ok "Transit signing key 'audit-log' created (type: ed25519)."
    fi

    # Create a policy allowing services to sign audit entries
    vault_auth_cmd policy write robin-audit-signer -<<EOF
path "transit/sign/audit-log" {
    capabilities = ["create", "update"]
}
path "transit/verify/audit-log" {
    capabilities = ["create", "update"]
}
EOF

    ok "Policy 'robin-audit-signer' created for audit log signing/verification."
}

# ============================================================================
# Main
# ============================================================================
main() {
    echo ""
    echo "================================================================"
    echo "  Robin Trading Platform — Vault Setup"
    echo "================================================================"
    echo "  VAULT_ADDR = $VAULT_ADDR"
    echo "================================================================"
    echo ""

    init_vault
    unseal_vault "${1:-/tmp/vault_init_keys.json}"
    mount_kv_v2
    configure_database_creds
    setup_approle
    setup_transit

    echo ""
    ok "Vault setup complete for Robin Trading Platform."
    echo ""
    info "Next steps:"
    info "  1. Distribute unseal keys to 5 trusted operators (key threshold: 3)."
    info "  2. Configure services to use AppRole login with the generated Role ID and Secret ID."
    info "  3. For production, enable audit logging: vault audit enable file file_path=/var/log/vault/audit.log"
    info "  4. Review and update policies in the vault UI or CLI as needed."
    echo ""
}

main "$@"
