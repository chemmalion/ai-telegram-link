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

* `/listprojects` to see saved projects.

### In a group with topics enabled

1. Start or enter a **topic/thread**.

2. As group admin, `@YourBot /settopic projectName`  
    → links this thread to that project.

3. Any plain message you send now will be forwarded to ChatGPT (GPT-3.5) under that API key.

4. Bot replies in-thread.

5. Use `/unsettopic` to disable.

## Docker

A `Dockerfile` and `docker-compose.yml` are included for container-based deployment.

1. Build the image:

```bash
docker build -t tgptbot .
```

2. (Optional) For local testing copy the sample environment file and edit the values:

```bash
cp env.example .env
# update BOT_TOKEN and MASTER_KEY
```

3. Run the container with a persistent data directory:

```bash
docker run --env-file .env -v $(pwd)/data:/data tgptbot
```

Alternatively start it with Docker Compose:

```bash
docker-compose up -d
```

The Bolt database will be stored under `data/` on the host.

## Deployment with AWS Secrets Manager

When running on an EC2 instance the bot can read its credentials directly from
AWS Secrets Manager instead of a local `.env` file. The helper script
`start-compose.sh` retrieves the secrets and starts the container using
Docker Compose. Set the secret names via the environment variables
`BOT_TOKEN_SECRET_ID` and `MASTER_KEY_SECRET_ID` and execute the script:

```bash
export BOT_TOKEN_SECRET_ID="my-bot-token-secret"
export MASTER_KEY_SECRET_ID="my-master-key-secret"
./start-compose.sh
```

The script requires `aws` CLI credentials to be available (for example via the
instance's IAM role).


