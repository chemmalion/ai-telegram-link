# Telegram–ChatGPT Bot in Go

This tool is implemented using ChatGPT.

## Prerequisites
- Go 1.24+
- Telegram Bot Token
- Base64-encoded 32-byte encryption key for `MASTER_KEY`

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
export BOT_TOKEN="123…"
export MASTER_KEY="base64-32-bytes"
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

* `/authproject <name>`
   → bot prompts you to send the OpenAI API key.

* Send the key as plain text.

* `/setmodel <projectName>`
  → choose the ChatGPT model for a project (defaults to ChatGPT 5).

* `/listprojects` to see saved projects.

### In a group with topics enabled

1. Start or enter a **topic/thread**.

2. As group admin, `@YourBot /settopic projectName`  
    → links this thread to that project.

3. Any plain message you send now will be forwarded to ChatGPT (GPT-5 by default) under that API key.

4. Bot replies in-thread.

5. Use `/unsettopic` to disable.

## Docker

A `Dockerfile` and `docker-compose.yml` are included for container based deployment.

1. Build the image:

```bash
docker build -t tgptbot .
```

2. Set the required environment variables before starting the container. When
running on AWS EC2 you can pull them from AWS Secrets Manager:

```bash
export BOT_TOKEN=$(aws secretsmanager get-secret-value --secret-id my-bot-token --query SecretString --output text)
export MASTER_KEY=$(aws secretsmanager get-secret-value --secret-id my-master-key --query SecretString --output text)
export TBOT_ALLOWED_USER_IDS=$(aws secretsmanager get-secret-value --secret-id my-allowed-users --query SecretString --output text)
```

For local development you may still create a `.env` file from `env.example` and
manually provide the values.

3. Run the container with a persistent data directory:

```bash
docker run -e BOT_TOKEN -e MASTER_KEY -e TBOT_ALLOWED_USER_IDS -v $(pwd)/data:/data tgptbot
```

Alternatively start it with Docker Compose, which will inherit the environment
variables set in your shell:

```bash
docker-compose up -d
```

The Bolt database will be stored under `data/` on the host.

## Deployment with AWS Secrets Manager

When running on an EC2 instance the bot can read its credentials directly from
AWS Secrets Manager instead of a local `.env` file. The helper script
`start-compose.sh` retrieves the secrets and starts the container using
Docker Compose. Set the secret names via the environment variables
`BOT_TOKEN_SECRET_ID`, `MASTER_KEY_SECRET_ID` and `TBOT_ALLOWED_USER_IDS_SECRET_ID` and execute the script:

```bash
export BOT_TOKEN_SECRET_ID="my-bot-token-secret"
export MASTER_KEY_SECRET_ID="my-master-key-secret"
export TBOT_ALLOWED_USER_IDS_SECRET_ID="my-allowed-users-secret"
./start-compose.sh
```

The script requires `aws` CLI credentials to be available (for example via the
instance's IAM role).


