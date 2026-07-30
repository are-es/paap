# PAAP — API Endpoints

## Base URLs

| Provider | Base URL | Format |
|---|---|---|
| Xiaomi MiMo | `https://api.xiaomimimo.com/v1` | OpenAI |
| DeepSeek | `https://api.deepseek.com/v1` | OpenAI |
| DeepSeek (Anthropic) | `https://api.deepseek.com/anthropic` | Anthropic |
| Kimchi | `https://llm.kimchi.dev/openai/v1` | OpenAI |
| OpenRouter | `https://openrouter.ai/api/v1` | OpenAI |
| AI Studio | `https://generativelanguage.googleapis.com/v1beta` | Google |

## Chat Completions

### OpenAI Format (Xiaomi, DeepSeek, Kimchi, OpenRouter)
```
POST {base_url}/chat/completions
```

**Request:**
```json
{
  "model": "model-name",
  "messages": [{"role": "user", "content": "Hello"}],
  "max_tokens": 100,
  "temperature": 0.7,
  "stream": false
}
```

**Response:**
```json
{
  "id": "chatcmpl-xxx",
  "object": "chat.completion",
  "model": "model-name",
  "choices": [{
    "index": 0,
    "message": {"role": "assistant", "content": "Hi!"},
    "finish_reason": "stop"
  }],
  "usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
}
```

### Anthropic Format (Xiaomi MiMo, DeepSeek — for Claude Code)
```
POST {base_url}/v1/messages
Header: x-api-key: {gateway_key}
Header: anthropic-version: 2023-06-01
```

**Request:**
```json
{
  "model": "mimo-v2.5-pro",
  "max_tokens": 1024,
  "system": "You are MiMo",
  "messages": [
    {"role": "user", "content": [{"type": "text", "text": "Hello"}]}
  ],
  "stream": false
}
```

**Response:**
```json
{
  "id": "msg-xxx",
  "type": "message",
  "model": "mimo-v2.5-pro",
  "content": [{"type": "text", "text": "Hi!"}],
  "stop_reason": "end_turn",
  "usage": {"input_tokens": 10, "output_tokens": 5}
}
```

**Claude Code config:**
```
ANTHROPIC_BASE_URL=http://localhost:9090
ANTHROPIC_API_KEY={gateway_key}
```

### Google Format (AI Studio)
```
POST {base_url}/models/{model}:generateContent?key={api_key}
```

**Request:**
```json
{
  "contents": [{"parts": [{"text": "Hello"}]}],
  "generationConfig": {"maxOutputTokens": 100}
}
```

## Models List

### OpenAI Format
```
GET {base_url}/models
```

### Google Format
```
GET {base_url}/models?key={api_key}
```
