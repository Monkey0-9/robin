#!/bin/bash
# generate_rsa_keys.sh
# Generates RS256 private and public keys for JWT authentication.

set -e

KEY_DIR="../config/keys"
mkdir -p "$KEY_DIR"

echo "Generating RSA Private Key (2048-bit)..."
openssl genrsa -out "$KEY_DIR/private.pem" 2048

echo "Extracting RSA Public Key..."
openssl rsa -in "$KEY_DIR/private.pem" -pubout -out "$KEY_DIR/public.pem"

echo "Keys generated successfully in $KEY_DIR"
echo "- private.pem (KEEP SECRET! Should be in HSM/Vault)"
echo "- public.pem  (Distribute to API Gateway)"
