# Free APIs Catalog

Use these APIs in `http_request` nodes when the user needs external data.
No API keys required — all are free tier with generous rate limits.

## Crypto Prices (CoinGecko)

- **Bitcoin price (USD)**:
  GET `https://api.coingecko.com/api/v3/simple/price?ids=bitcoin&vs_currencies=usd`
  Response: `{ "bitcoin": { "usd": 70123.45 } }`
  Extract price: `{{steps['<node_id>'].output.body}}`  → parse JSON → `.bitcoin.usd`

- **Any coin price**:
  GET `https://api.coingecko.com/api/v3/simple/price?ids={coin_id}&vs_currencies=usd`
  Common coin IDs: bitcoin, ethereum, solana, dogecoin, cardano, polkadot, ripple
  Response: `{ "<coin_id>": { "usd": <price> } }`

- **Multiple coins at once**:
  GET `https://api.coingecko.com/api/v3/simple/price?ids=bitcoin,ethereum,solana&vs_currencies=usd`

- **Rate limit**: 10-30 requests/minute on free tier. Safe for cron intervals >= 5 minutes.

## Weather (wttr.in)

- **Current weather (JSON)**:
  GET `https://wttr.in/{city}?format=j1`
  Response includes: `current_condition[0].temp_C`, `current_condition[0].weatherDesc[0].value`
  Example cities: London, NewYork, Tokyo, Mumbai (use URL-safe names, no spaces)

- **One-line weather**:
  GET `https://wttr.in/{city}?format=%t+%C`
  Returns plain text like: `+22°C Partly cloudy`

## Exchange Rates

- **Latest rates**:
  GET `https://api.exchangerate-api.com/v4/latest/{base_currency}`
  Example: `https://api.exchangerate-api.com/v4/latest/USD`
  Response: `{ "rates": { "EUR": 0.92, "GBP": 0.79, ... } }`

## IP & Geolocation

- **Current IP info**:
  GET `https://ipapi.co/json/`
  Response: `{ "ip": "...", "city": "...", "country_name": "...", "latitude": ..., "longitude": ... }`

- **Specific IP lookup**:
  GET `https://ipapi.co/{ip}/json/`

## Random Data & Placeholders

- **Random user**:
  GET `https://randomuser.me/api/`
  Response: `{ "results": [{ "name": { "first": "...", "last": "..." }, "email": "..." }] }`

- **Placeholder posts** (testing):
  GET `https://jsonplaceholder.typicode.com/posts/1`
  Response: `{ "userId": 1, "id": 1, "title": "...", "body": "..." }`

## Public Data

- **GitHub user info**:
  GET `https://api.github.com/users/{username}`
  Response: `{ "login": "...", "name": "...", "public_repos": 42, ... }`
  Note: 60 requests/hour unauthenticated. For higher limits, user should save a GitHub token as a secret.

- **Hacker News top stories**:
  GET `https://hacker-news.firebaseio.com/v0/topstories.json`
  Returns array of story IDs. Fetch individual: GET `https://hacker-news.firebaseio.com/v0/item/{id}.json`

## Usage Instructions for the LLM

- When the user mentions a data source covered here, use an `http_request` node with the exact URL.
- Do NOT ask the user for API details if the API is listed here.
- Parse JSON responses using template syntax: `{{steps['node_id'].output.body}}`
- For APIs requiring authentication (not listed here), use `_ref` secret references and tell the user to add the secret in Settings → Secrets.
