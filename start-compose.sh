#!/bin/bash
set -euo pipefail

# Retrieve all required values from a single AWS Secrets Manager secret
SECRET_JSON=$(aws secretsmanager get-secret-value --secret-id "$TBOT_SECRET_ID" --query SecretString --output text)

# Extract individual fields without creating intermediate files
TBOT_TELEGRAM_KEY=$(jq -r '."tbot-telegram-access-token"' <<<"$SECRET_JSON")
TBOT_MASTER_KEY=$(jq -r '."tbot-master-key"' <<<"$SECRET_JSON")
TBOT_CHATGPT_KEY=$(jq -r '."tbot-chatgpt-key"' <<<"$SECRET_JSON")
TBOT_ALLOWED_USER_IDS=$(jq -r '."tbot-allowed-user-ids"' <<<"$SECRET_JSON")
export TBOT_TELEGRAM_KEY TBOT_MASTER_KEY TBOT_CHATGPT_KEY TBOT_ALLOWED_USER_IDS
exec docker compose up -d
