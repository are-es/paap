# Meta Model API — Provider Reference

> Docs: https://dev.meta.ai/docs/getting-started/overview/
> Base URL: https://api.meta.ai/v1
> Model: muse-spark-1.1 (1M context)

## Auth

- **Key format**: `LLM|<numeric_id>|<secret>` (e.g. `LLM|607358788850350|nx9...LJY`)
- **Header**: `Authorization: Bearer LLM|...`
- **Storage**: env `MODEL_API_KEY` or PAAP api_keys table

Meta keys share rate limits per **team**, not per key. Leak = burn team quota.

## Endpoints (OpenAI-compatible)

- `POST /v1/chat/completions` — chat (supports reasoning_effort)
- `POST /v1/responses` — Responses API (full features: search grounding, file inputs, reasoning replay)
- `GET /v1/models` — list models
- `GET /v1/files` — Files API

PAAP uses `/v1/chat/completions` by default (simpler, OpenAI-compatible).

## Models

| Model ID | Input | Output | Context | Best for |
|---|---|---|---|---|
| `muse-spark-1.1` | text, image, video, PDF | text | 1,048,576 | agentic tool calling, coding, reasoning |

List via: `curl https://api.meta.ai/v1/models -H "Authorization: Bearer $MODEL_API_KEY"`

## Reasoning

Muse Spark is reasoning model — always thinks before answering.

- **Param**: `reasoning_effort` top-level (Chat Completions) or `reasoning.effort` nested (Responses)
- **Values**: `minimal`, `low`, `medium`, `high`, `xhigh` — `none` NOT supported (400)
- **PAAP normalization**: 
  - `none` → `low` (auto)
  - No value + Meta provider → inject `low` (cheapest, safe)
  - Client sends `high/xhigh` → forwarded as-is

Higher effort = more tokens, latency, cost. Use `low` for direct-answer, `high` for complex proofs/code.

- **Summaries**: `reasoning.summary = auto|concise|detailed` (Responses only)
- **Multi-turn**: 
  - Responses: `previous_response_id` or encrypted replay `include: ["reasoning.encrypted_content"]`
  - Chat Completions: NO continuity (reasoning redacted to empty for external callers)

## Pricing

| Usage | $ / 1M tokens |
|---|---|
| Input | $1.25 |
| Cached input | $0.15 |
| Output | $4.25 |
| Web search grounding | $2.50 / 1k queries |

No long-context premium. Same rate empty or full.

## Rate Limits

| Tier | RPM | TPM |
|---|---|---|
| Free | 60 | 2,000,000 |
| Paid | 3,000 | 4,000,000 |

- Per **team**, not per key
- Headers: `x-ratelimit-limit-tokens`, `x-ratelimit-remaining-tokens`, `x-ratelimit-limit-requests`, `x-ratelimit-remaining-requests`
- 429 → exponential backoff with jitter, check Retry-After
- Background submissions (Responses `background:true`) has separate 600 req/min limit

## API Examples

### curl
```bash
curl -X POST "https://api.meta.ai/v1/chat/completions"   -H "Authorization: Bearer $MODEL_API_KEY"   -H "Content-Type: application/json"   -d '{
    "model": "muse-spark-1.1",
    "reasoning_effort": "high",
    "messages": [{"role": "user", "content": "Prove sqrt(2) is irrational"}]
  }'
```

### Python (OpenAI SDK)
```python
import os
from openai import OpenAI
client = OpenAI(
    base_url="https://api.meta.ai/v1",
    api_key=os.environ["MODEL_API_KEY"],
)
response = client.chat.completions.create(
    model="muse-spark-1.1",
    reasoning_effort="low",
    messages=[{"role": "user", "content": "What is capital of France?"}]
)
print(response.choices[0].message.content)
```

### PAAP via gateway
```bash
curl -X POST "http://localhost:9090/v1/chat/completions"   -H "Authorization: Bearer sk-<your-gateway-key>"   -H "Content-Type: application/json"   -d '{
    "model": "muse-spark-1.1",
    "reasoning_effort": "low",
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

## Error Handling

- 400 — invalid param (e.g. `reasoning_effort: none`, `top_p: 0`, `n>1`, `logprobs:true`), context exceeded, invalid structure
- 401 — invalid/missing key (`LLM|...` format)
- 402 — billing issue
- 403 — no permission for model
- 404 — model/file not found (`code: model_not_found`, `file_not_found`)
- 413 — body too large for background mode (1 MiB)
- 429 — rate limit (`code: rate_limit_exceeded`) + Retry-After
- 500 — transient, retry with backoff
- 503 — shutting down (`code: server_shutting_down`) + Retry-After, retryable
- 504 — non-streaming timeout → use `stream:true`

## PAAP Integration Details

- **Provider name**: `Meta`
- **Base URL**: `https://api.meta.ai/v1`
- **Icon**: `meta.svg` (blue with M)
- **Key validation**: `LLM|` format check + live `/v1/models` test
- **Models seeded**: `muse-spark-1.1`
- **Cost calc**: $1.25 / $4.25 per 1M
- **Reasoning handling**: auto `none`→`low`, default inject `low` if missing
- **Proxy**: supported via existing pool (Bearer auth)
- **No body converter needed** (OpenAI-compatible unlike Merlin)

## Limitations vs Other Providers

- Only 1 model currently (vs dozens on OpenRouter/Xiaomi)
- Reasoning tokens count toward output budget — need high `max_tokens` (PAAP injects 20k min)
- No `logprobs`, no `stop`, no `n>1`, no `audio`, no `modalities` — returns 400
- Search grounding only via Responses API, not Chat Completions
- Thinking phase not streamed as text — expect initial latency before first token

## Next

- Add tool calling / file API / search grounding via Responses endpoint (future PAAP phase)
- Prompt caching: `prompt_cache_retention: in_memory|24h`
