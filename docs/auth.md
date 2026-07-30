# PAAP — Authentication

## Auth Methods

| Provider | Method | Header | Key Format |
|---|---|---|---|
| Xiaomi MiMo | Bearer Token | `Authorization: Bearer sk-...` | `sk-xxx` |
| DeepSeek | Bearer Token | `Authorization: Bearer sk-...` | `sk-xxx` |
| Kimchi | Bearer Token + UA | `Authorization: Bearer castai_v1_...` | `castai_v1_xxx` |
| OpenRouter | Bearer Token | `Authorization: Bearer sk-or-...` | `sk-or-xxx` |
| AI Studio | API Key | `x-goog-api-key: AIzaSy...` | `AIzaSy...` |

## Headers Required

### Xiaomi MiMo
```http
Content-Type: application/json
Authorization: Bearer sk-YOUR_KEY
```

### DeepSeek
```http
Content-Type: application/json
Authorization: Bearer sk-YOUR_KEY
```

### Kimchi (CRITICAL)
```http
Content-Type: application/json
Authorization: Bearer castai_v1_YOUR_KEY
User-Agent: kimchi/0.1.50
```
**⚠️ Without `User-Agent: kimchi/0.1.50`, Kimchi returns 403/402.**

### OpenRouter (optional headers)
```http
Content-Type: application/json
Authorization: Bearer sk-or-YOUR_KEY
HTTP-Referer: https://your-app.com
X-OpenRouter-Title: Your App Name
```

### AI Studio
```http
Content-Type: application/json
x-goog-api-key: AIzaSy_YOUR_KEY
```
Or via query param: `?key=AIzaSy_YOUR_KEY`

## Error Codes

| Code | Meaning | Action |
|---|---|---|
| 401 | Unauthorized | Check API key |
| 403 | Forbidden | Check headers (Kimchi: missing User-Agent) |
| 429 | Rate Limited | Backoff and retry |
| 500 | Server Error | Retry after delay |
