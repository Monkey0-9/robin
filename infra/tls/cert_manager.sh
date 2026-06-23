#!/usr/bin/env bash
# Robin Platform - Security Infrastructure Setup
# Generates local CA and certificates for mTLS, and configures HashiCorp Vault.

set -e

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
CERTS_DIR="$DIR/certs"
mkdir -p "$CERTS_DIR"

echo "[Security] Generating local Certificate Authority (CA) for mTLS..."
openssl req -x509 -newkey rsa:4096 -days 365 -nodes \
    -keyout "$CERTS_DIR/ca.key" -out "$CERTS_DIR/ca.crt" \
    -subj "/C=US/ST=NY/L=New York/O=Robin Platform/CN=Robin Internal CA"

function generate_service_cert {
    SERVICE=$1
    echo "[Security] Generating mTLS certificate for $SERVICE..."
    openssl genrsa -out "$CERTS_DIR/$SERVICE.key" 2048
    openssl req -new -key "$CERTS_DIR/$SERVICE.key" -out "$CERTS_DIR/$SERVICE.csr" \
        -subj "/C=US/ST=NY/L=New York/O=Robin Platform/CN=$SERVICE"
    openssl x509 -req -in "$CERTS_DIR/$SERVICE.csr" \
        -CA "$CERTS_DIR/ca.crt" -CAkey "$CERTS_DIR/ca.key" -CAcreateserial \
        -out "$CERTS_DIR/$SERVICE.crt" -days 365
}

generate_service_cert "orchestrator"
generate_service_cert "matching-engine"
generate_service_cert "risk-analytics"
generate_service_cert "compliance"

echo "[Security] Vault Integration Stub..."
cat << 'EOF' > "$DIR/vault_agent.hcl"
pid_file = "./pidfile"

vault {
  address = "http://127.0.0.1:8200"
}

auto_auth {
  method "approle" {
    mount_path = "auth/approle"
    config = {
      role_id_file_path = "./roleid"
      secret_id_file_path = "./secretid"
      remove_secret_id_file_after_reading = false
    }
  }
}

template {
  source      = "./secrets.ctmpl"
  destination = "./.env"
}
EOF

echo "[Security] Security infrastructure setup complete."
