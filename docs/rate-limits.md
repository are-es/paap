# PAAP — Rate Limits

## Provider Rate Limits

| Provider | RPM | TPM | RPD | Concurrency |
|---|---|---|---|---|
| Xiaomi MiMo | 100 | 10M | - | - |
| DeepSeek | - | - | - | 2500 (flash) / 500 (pro) |
| Kimchi | Dynamic | Dynamic | - | - |
| OpenRouter (free) | 20 | - | 50-1000 | - |
| AI Studio (free) | 15 | - | - | - |

## Notes

- **Kimchi:** No fixed limits, uses queue system. Speed depends on upstream provider.
- **OpenRouter:** 50 RPD if <10 credits purchased, 1000 RPD if ≥10 credits
- **AI Studio:** Measured per project, not per API key
- **DeepSeek:** Free capacity expansion available on request

## Best Practices

1. **Implement exponential backoff** on 429 errors
2. **Use cache hit** when possible (99% cheaper)
3. **Rotate API keys** if hitting rate limits
4. **Monitor usage** via provider dashboards
