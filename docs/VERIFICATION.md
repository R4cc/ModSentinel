# Dashboard Verification

## Manual Scenarios

1. **With Modrinth token**
   - Start the Go backend: `go run .`.
   - In another terminal, run the frontend: `npm --prefix frontend run dev`.
   - Navigate to `http://localhost:5173` and ensure dashboard data loads without alerts.

2. **Without token**
   - Remove the token from local storage: `localStorage.removeItem('modrinth-token');`.
   - Reload the dashboard. The Alerts card should show "Modrinth token required." with an **Open Settings** action.

3. **Slow network**
   - Use browser devtools to throttle the network to "Slow 3G" and reload the dashboard.
   - Skeleton placeholders should persist until data arrives and the layout should not shift.

4. **429 Rate limit / 5xx errors**
   - Temporarily stop the backend or send many rapid requests to `/api/dashboard`.
   - The Alerts card should display "Rate limit hit." for 429 or "Failed to load data." for 5xx responses and offer a **Retry** button.

## Automated Tests

Run from the repository root:

```bash
npm --prefix frontend test
```

The suite covers:
- dashboard data refresh caching
- optimistic update rollback on failed mod updates
- alert visibility for missing tokens and rate limit errors
