# Step 1: a second server, and sessions that any server can read

**The failure.** One app is one point of failure, and one CPU. A second app should help. It does not, as long as sessions live in each app's memory: you log in on one app, the next request goes to the other, and the other has never seen you (`401`).

**The part.** Sessions move to one shared store (Redis). A load balancer (Caddy) spreads requests over `app1` and `app2`. Any app can serve any user, so the apps hold no state of their own: a *stateless* web tier.

**What changed**

- `internal/session`: `Store` is now an interface, with `Memory` (this process only) and `Redis` (shared).
- `cmd/app`: `SESSIONS=redis` (default) or `SESSIONS=memory`.
- `compose.yaml`: `state` (Redis), `app1`, `app2`, `lb`. The load balancer is the only way in, on `localhost:8088`.
- `lb/Caddyfile`: round robin; if an app cannot be reached, the request goes to the other one.

**Run the demos**

```sh
make up SESSIONS=memory && make seed && make demo-sessions NAME=sessions-memory   # about half of the creates get a 401
make up                 && make seed && make demo-sessions NAME=sessions-shared   # none do
make demo-kill                                                                   # kill app2 under load
make load                                                                        # watch it fail: now the database is the next limit
```

**Skip it until** you want more than one server, or want to survive the loss of one.
