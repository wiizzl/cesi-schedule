# cesi-schedule

Serves your [CESI](https://cesi.fr) timetable as an iCalendar (`.ics`) feed.

Open the site, enter your CESI email and password once, and you get a private subscription link:

```
GET /calendar.ics?token=<token>
```

The feed covers the previous week and the next 7 weeks.

The token holds your credentials encrypted (AES-256-GCM) with the server secret: the password never appears
in URLs or logs, and nothing is stored server-side besides short-lived encrypted caches. Changing your CESI
password invalidates the link; generate a new one.

## Configuration

| Variable                                              | Description                                                                                                                           |
| ----------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| `CESI_SECRET`                                         | required, ≥ 32 random chars (`openssl rand -base64 32`). Changing it invalidates every link.                                          |
| `UPSTASH_REDIS_REST_URL` / `UPSTASH_REDIS_REST_TOKEN` | required Upstash Redis (`KV_REST_API_*` also work). The server refuses to start without it and answers 503 if it becomes unreachable. |
| `PORT`                                                | listen port for `cmd/server` (default `3000`)                                                                                         |

## Protecting students and CESI

- Calendars are cached 15 min.
- Credentials rejected by CESI are not retried for 6 h, so a calendar app polling with an old password
  cannot lock the account.
- Limits: 5 link generations per IP per 15 min, 30 upstream refreshes per user per hour, 8 concurrent
  ADFS logins per instance.
