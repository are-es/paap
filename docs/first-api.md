# PAAP — First API Call Guide

## 1. Xiaomi MiMo

**Register:** https://platform.xiaomimimo.com
1. Sign up with email
2. Go to API Keys page
3. Create new key
4. Copy key (format: `sk-...`)

**First Call:**
```bash
curl https://api.xiaomimimo.com/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-YOUR_KEY" \
  -d '{"model":"mimo-v2.5-pro","messages":[{"role":"user","content":"Hello"}]}'
```

---

## 2. DeepSeek

**Register:** https://platform.deepseek.com
1. Sign up with email
2. Go to API Keys page
3. Create new key
4. Copy key (format: `sk-...`)

**First Call:**
```bash
curl https://api.deepseek.com/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-YOUR_KEY" \
  -d '{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"Hello"}]}'
```

---

## 3. Kimchi

**Register:** https://kimchi.dev
1. Sign up with email
2. Go to API Keys page
3. Create new key (format: `castai_v1_...`)
4. **Important:** Set `User-Agent: kimchi/0.1.50` header

**First Call:**
```bash
curl https://llm.kimchi.dev/openai/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer castai_v1_YOUR_KEY" \
  -H "User-Agent: kimchi/0.1.50" \
  -d '{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"Hello"}]}'
```

---

## 4. OpenRouter

**Register:** https://openrouter.ai
1. Sign up with email/GitHub
2. Go to Keys page
3. Create new key (format: `sk-or-...`)
4. Free tier available (no payment required)

**First Call:**
```bash
curl https://openrouter.ai/api/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-or-YOUR_KEY" \
  -d '{"model":"nvidia/nemotron-3-ultra-550b-a55b:free","messages":[{"role":"user","content":"Hello"}]}'
```

---

## 5. Google AI Studio

**Register:** https://aistudio.google.com
1. Sign in with Google account
2. Go to API Keys page
3. Create new key
4. Copy key (format: `AIzaSy...`)

**First Call:**
```bash
curl "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent?key=YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{"contents":[{"parts":[{"text":"Hello"}]}]}'
```
