#!/usr/bin/env bash
# Generates a self-signed TLS cert for local development.
# The SAN extension makes it work with Go's strict cert verification.
set -e

CERTS_DIR="$(dirname "$0")/../certs"
mkdir -p "$CERTS_DIR"

openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
    -keyout "$CERTS_DIR/server.key" \
    -out    "$CERTS_DIR/server.crt" \
    -subj   "/CN=messenger-server/O=CourseWork" \
    -addext "subjectAltName=IP:127.0.0.1,DNS:localhost"

echo "Certs written to $CERTS_DIR/"
