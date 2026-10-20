# cesi-schedule

Serves your [CESI](https://www.cesi.fr) timetable as an iCalendar (`.ics`) feed. Stateless: credentials
are passed in the URL on every request, nothing is stored.

```
GET /?username=<email>&password=<password>[&weeks=4][&start=YYYY-MM-DD&end=YYYY-MM-DD]
```

| Param           | Default                 | Description                  |
| --------------- | ----------------------- | ---------------------------- |
| `username`      |                         | CESI email (required)        |
| `password`      |                         | CESI password (required)     |
| `weeks`         | `4`                     | number of weeks (1–52)       |
| `start` / `end` | current week → +`weeks` | explicit range, `YYYY-MM-DD` |
