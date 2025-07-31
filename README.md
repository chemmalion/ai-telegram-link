# Telegram–ChatGPT Bot in Go

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

