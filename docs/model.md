# PAAP — Models Reference

## 1. Xiaomi MiMo

| Model | Input/1M (Cache Hit) | Input/1M (Cache Miss) | Output/1M | Context | Features |
|---|---|---|---|---|---|
| mimo-v2.5-pro | $0.0036 | $0.435 | $0.87 | 1M | Text, Tools, Thinking |
| mimo-v2.5 | $0.0028 | $0.14 | $0.28 | 1M | Text, Tools, Thinking |
| mimo-v2.5-asr | - | - | - | - | Audio Speech Recognition |
| mimo-v2.5-tts | - | - | - | - | Text-to-Speech |
| mimo-v2.5-tts-voiceclone | - | - | - | - | Voice Cloning |
| mimo-v2.5-tts-voicedesign | - | - | - | - | Voice Design |

**Rate Limits:** 100 RPM, 10M TPM

---

## 2. DeepSeek

| Model | Input/1M (Cache Hit) | Input/1M (Cache Miss) | Output/1M | Context | Features |
|---|---|---|---|---|---|
| deepseek-v4-flash | $0.0028 | $0.14 | $0.28 | 1M | Text, Tools, Thinking, FIM |
| deepseek-v4-pro | $0.003625 | $0.435 | $0.87 | 1M | Text, Tools, Thinking, FIM |

**Rate Limits:** 2500 concurrency (flash), 500 concurrency (pro)

---

## 3. Kimchi (llm.kimchi.dev)

| Model | Input/1M | Output/1M | Notes |
|---|---|---|---|
| deepseek-v4-flash | $0.14 | $0.28 | via Kimchi proxy |
| minimax-m3 | $0.51 | $2.04 | |
| nemotron-3-ultra-fp4 | $0.60 | $3.60 | |
| kimi-k2.7 | $0.95 | $4.00 | |
| glm-5.2-fp8 | $1.40 | $4.40 | |

**Auth:** `User-Agent: kimchi/0.1.50` required
**Base URL:** `https://llm.kimchi.dev/openai/v1`

---

## 4. OpenRouter (Free Models)

| Model | Context | Input/1M | Output/1M |
|---|---|---|---|
| nvidia/nemotron-3-ultra-550b-a55b:free | 1M | $0 | $0 |
| nvidia/nemotron-3-super-120b-a12b:free | 1M | $0 | $0 |
| poolside/laguna-m.1:free | 262K | $0 | $0 |
| qwen/qwen3-coder:free | 1M | $0 | $0 |
| google/gemma-4-31b-it:free | 262K | $0 | $0 |
| meta-llama/llama-3.3-70b-instruct:free | 131K | $0 | $0 |
| openai/gpt-oss-120b:free | 131K | $0 | $0 |
| openai/gpt-oss-20b:free | 131K | $0 | $0 |
| nvidia/nemotron-nano-12b-v2-vl:free | 128K | $0 | $0 |
| cohere/north-mini-code:free | 256K | $0 | $0 |

**Rate Limits:** 20 RPM (free), 1000 RPD (if ≥10 credits purchased)
**Base URL:** `https://openrouter.ai/api/v1`

---

## 5. Google AI Studio

| Model | Input/1M | Output/1M | Context |
|---|---|---|---|
| gemini-3.5-flash | Free | Free | 1M |
| gemini-2.5-pro | Free | Free | 1M |
| gemini-2.5-flash | Free | Free | 1M |
| gemini-2.5-flash-lite | Free | Free | 1M |
| gemini-3.1-flash-lite | Free | Free | 1M |
| gemma-4 | Free | Free | 1M |

**Rate Limits:** 15 RPM (free tier)
**Base URL:** `https://generativelanguage.googleapis.com/v1beta`
**Auth:** API key via `x-goog-api-key` header
