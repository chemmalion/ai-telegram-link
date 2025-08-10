# Telegram–ChatGPT Bot in Go

This tool is implemented using ChatGPT.

## Prerequisites
- Go 1.24+
- Telegram Bot Token
- ChatGPT API key
- Base64-encoded 32-byte encryption key for `TBOT_MASTER_KEY`

```bash
# generate a random 32-byte key:
head -c 32 /dev/urandom | base64
```

## Setup

Clone repo:

```bash
git clone https://your.repo/telegram-chatgpt-bot.git
cd telegram-chatgpt-bot
```

1.

Set environment variables:

```bash
export TBOT_TELEGRAM_KEY="123…"
export TBOT_CHATGPT_KEY="sk-..."
export TBOT_MASTER_KEY="base64-32-bytes"
export TBOT_ALLOWED_USER_IDS="12345,67890"
export LOG_LEVEL="info" # optional: debug, info, warn, error
```

2.

Build and run:

```bash
go mod tidy
go build -o tgptbot ./cmd/tgptbot
./tgptbot
```

3.

## Usage

### In private chat with bot

* `/newproject <name>`
  → register a new project.

* `/setmodel <projectName>`
  → choose the ChatGPT model for a project (defaults to ChatGPT 5).

* `/listprojects` to see saved projects.

### In a group with topics enabled

1. Start or enter a **topic/thread**.

2. As group admin, `@YourBot /settopic projectName`  
    → links this thread to that project.

3. Any plain message you send now will be forwarded to ChatGPT (GPT-5 by default) using the global API key.

4. Bot replies in-thread.

5. Use `/unsettopic` to disable.

## Docker and AWS

When running on an EC2 instance the bot can read its credentials directly from
AWS Secrets Manager instead of a local `.env` file. The helper script
`start-compose.sh` retrieves a single secret containing the JSON keys
`tbot-telegram-access-token`, `tbot-master-key`, `tbot-chatgpt-key` and `tbot-allowed-user-ids` and
starts the container using Docker Compose. Set the secret name via the
environment variable `TBOT_SECRET_ID` and execute the script:

The script requires `aws` CLI credentials to be available (for example via the
instance's IAM role).

### Shorthand script

```bash
export TBOT_DATA_PATH=$(pwd)/data
export TBOT_SECRET_ID=my-tbot-secret
./start-compose.sh
```

### Manual run with docker (alternative to shorthand)

A `Dockerfile` and `docker-compose.yml` are included for container based deployment.

1. Build the image:

```bash
docker build -t tgptbot .
```

2. Set the required environment variables before starting the container. When
running on AWS EC2 you can pull them from a single AWS Secrets Manager secret
that stores all values as JSON with keys `tbot-telegram-access-token`,
`tbot-master-key`, `tbot-chatgpt-key` and `tbot-allowed-user-ids`:

```bash
SECRET_JSON=$(aws secretsmanager get-secret-value --secret-id my-tbot-secret --query SecretString --output text)
export TBOT_TELEGRAM_KEY=$(echo "$SECRET_JSON" | jq -r '."tbot-telegram-access-token"')
export TBOT_MASTER_KEY=$(echo "$SECRET_JSON" | jq -r '."tbot-master-key"')
export TBOT_CHATGPT_KEY=$(echo "$SECRET_JSON" | jq -r '."tbot-chatgpt-key"')
export TBOT_ALLOWED_USER_IDS=$(echo "$SECRET_JSON" | jq -r '."tbot-allowed-user-ids"')
```

For local development you may still create a `.env` file from `env.example` and
manually provide the values.

3. Run the container with a persistent data directory:

```bash
docker run -e TBOT_TELEGRAM_KEY -e TBOT_MASTER_KEY -e TBOT_CHATGPT_KEY -e TBOT_ALLOWED_USER_IDS -v $(pwd)/data:/data tgptbot
```

Alternatively start it with Docker Compose, which will inherit the environment
variables set in your shell:

```bash
export TBOT_DATA_PATH=$(pwd)/data
docker compose up -d
```

The Bolt database will be stored under `data/` on the host.
