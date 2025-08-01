#!/bin/bash
set -euo pipefail

BOT_TOKEN=$(aws secretsmanager get-secret-value --secret-id "$BOT_TOKEN_SECRET_ID" --query SecretString --output text)
MASTER_KEY=$(aws secretsmanager get-secret-value --secret-id "$MASTER_KEY_SECRET_ID" --query SecretString --output text)
if [ -n "${TBOT_ALLOWED_USER_IDS_SECRET_ID:-}" ]; then
    TBOT_ALLOWED_USER_IDS=$(aws secretsmanager get-secret-value --secret-id "$TBOT_ALLOWED_USER_IDS_SECRET_ID" --query SecretString --output text)
fi

export BOT_TOKEN MASTER_KEY TBOT_ALLOWED_USER_IDS
exec docker-compose up -d

