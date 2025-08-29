# Runbook

## "context canceled"

A Modrinth request may log an error similar to:

```
{"level":"info","event":"modrinth_error","error":"Get \"https://api.modrinth.com/v2/search?query=scalablelux\": context canceled"}
```

### Likely Causes
- The client aborted the HTTP request (e.g., browser tab closed or navigation away).
- The server shut down or the parent context was canceled, ending outstanding calls.
- A timeout expired before the Modrinth request completed.

### Step-by-Step Checks
1. **Confirm the log entry**
   - Search recent logs for `context canceled` to verify when and where it occurred.
2. **Determine caller behaviour**
   - Check whether the initiating client disconnected or canceled the request.
   - Reproduce with a persistent client (e.g., `curl`) to rule out premature cancellation by the caller.
3. **Inspect timeouts and shutdowns**
   - Ensure server timeouts are sufficiently long for typical Modrinth latency.
   - Confirm the server or worker wasn't shutting down at the time of the error.
4. **Check network stability**
   - Look for signs of connectivity issues between the server and Modrinth.
5. **Verify worker continuation**
   - For background jobs, confirm they continue processing despite the canceled request by checking job status and progress.

### Resolution
- If user cancellations are expected, no action is required.
- For repeated unexpected cancellations, adjust timeouts, investigate network issues, or review shutdown procedures to ensure requests complete.

