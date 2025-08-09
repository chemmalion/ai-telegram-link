#!/bin/bash
set -euo pipefail

# Retrieve all required values from a single AWS Secrets Manager secret
SECRET_JSON=$(aws secretsmanager get-secret-value --secret-id "$TBOT_SECRET_ID" --query SecretString --output text)

# Extract individual fields without creating intermediate files
BOT_TOKEN=$(jq -r '."tbot-telegram-access-token"' <<<"$SECRET_JSON")
MASTER_KEY=$(jq -r '."tbot-master-key"' <<<"$SECRET_JSON")
TBOT_ALLOWED_USER_IDS=$(jq -r '."tbot-allowed-user-ids"' <<<"$SECRET_JSON")

export BOT_TOKEN MASTER_KEY TBOT_ALLOWED_USER_IDS
exec docker compose up -d

