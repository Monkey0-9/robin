#!/bin/bash
# setup_mtls.sh
# Generates Certificate Authority (CA), Server, and Client certificates for mTLS.

set -e

CERTS_DIR="../config/certs"
mkdir -p "$CERTS_DIR"
cd "$CERTS_DIR"

echo "1. Generating CA..."
openssl genrsa -out ca.key 2048
openssl req -new -x509 -days 3650 -key ca.key -out ca.crt -subj "/CN=Robin Local CA"

echo "2. Generating Server Certificate..."
openssl genrsa -out server.key 2048
openssl req -new -key server.key -out server.csr -subj "/CN=localhost"
openssl x509 -req -days 365 -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out server.crt

echo "3. Generating Client Certificate..."
openssl genrsa -out client.key 2048
openssl req -new -key client.key -out client.csr -subj "/CN=Robin Test Client"
openssl x509 -req -days 365 -in client.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out client.crt

echo "mTLS Certificates generated successfully in $CERTS_DIR"
