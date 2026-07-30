# PAAP — Cost Comparison

## Cheapest Models (Input/1M tokens)

| Rank | Provider | Model | Input/1M | Output/1M |
|---|---|---|---|---|
| 1 | OpenRouter | *:free models | $0.00 | $0.00 |
| 2 | AI Studio | gemini-2.5-flash | Free | Free |
| 3 | DeepSeek | deepseek-v4-flash (cache) | $0.0028 | $0.28 |
| 4 | Xiaomi | mimo-v2.5 (cache) | $0.0028 | $0.28 |
| 5 | DeepSeek | deepseek-v4-flash (miss) | $0.14 | $0.28 |
| 6 | Xiaomi | mimo-v2.5 (miss) | $0.14 | $0.28 |
| 7 | Kimchi | deepseek-v4-flash | $0.14 | $0.28 |
| 8 | Kimchi | minimax-m3 | $0.51 | $2.04 |
| 9 | Xiaomi | mimo-v2.5-pro (miss) | $0.435 | $0.87 |
| 10 | DeepSeek | deepseek-v4-pro (miss) | $0.435 | $0.87 |

## Cost Estimation (1M input + 100K output)

| Provider | Model | Cost |
|---|---|---|
| OpenRouter | free models | $0.00 |
| AI Studio | gemini-2.5-flash | $0.00 |
| DeepSeek | deepseek-v4-flash | $0.017 |
| Xiaomi | mimo-v2.5 | $0.017 |
| Kimchi | deepseek-v4-flash | $0.042 |
| DeepSeek | deepseek-v4-pro | $0.052 |
| Xiaomi | mimo-v2.5-pro | $0.052 |
| Kimchi | minimax-m3 | $0.255 |

## Recommendations

- **Development/Testing:** OpenRouter free models ($0)
- **Production (cheap):** DeepSeek/Xiaomi cache hit ($0.0028/1M)
- **Production (quality):** DeepSeek/Xiaomi pro ($0.435/1M)
- **Proxy relay:** Kimchi (slightly more expensive, but convenient)
