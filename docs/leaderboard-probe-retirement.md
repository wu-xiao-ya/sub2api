# Leaderboard And Probe Retirement

The leaderboard reads the real input/output/cache token columns, excludes
monitoring identities and monitoring traffic, and never changes billing data.
It ranks the available detail records; missing historical detail is not
fabricated from hourly summaries. Today, the current Monday-based week, and
the current calendar month use the requesting user's timezone.

The independent intelligence probe has been retired:

- No public or admin probe routes are registered.
- Dependency injection does not configure a gateway or start its worker.
- No probe gallery, settings panel, or API client is reachable from the UI.
- Existing probe settings, targets, results, and migrations are retained.
- Server monitoring and channel availability monitoring remain independent.

Keep the current history-preservation deployment environment unchanged.
The frontend build merges content-hashed assets into the embedded directory so
existing browser sessions can still fetch their cached version's dependencies.
The new entrypoint no longer imports any retired probe interface.
