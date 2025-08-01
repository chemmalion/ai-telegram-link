#!/bin/bash
set -euo pipefail

BOT_TOKEN=$(aws secretsmanager get-secret-value --secret-id "$BOT_TOKEN_SECRET_ID" --query SecretString --output text)
MASTER_KEY=$(aws secretsmanager get-secret-value --secret-id "$MASTER_KEY_SECRET_ID" --query SecretString --output text)

export BOT_TOKEN MASTER_KEY
exec docker-compose up -d

