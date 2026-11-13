# Step 5: the service tells you how it is doing

**The need.** Until now everything was watched from outside, with a load test. A running service has to say how it is doing itself.

**The parts**
- `GET /healthz` answers `ok` while the app answers. The load balancer calls it every second (HAProxy `option httpchk`) and takes an app out after two failed checks. It is shallow on purpose: it does not call the database.
- `GET /metrics` prints the four golden signals of the Google SRE book, one metric each (Prometheus text format):
  - latency: `http_request_duration_seconds`
  - traffic: `http_requests_total`
  - errors: `http_errors_total`
  - saturation: `http_requests_in_flight`

  The label is the route that matched (`GET /{code}`), never the URL, so there is one series for each route and not one for each short link.

For a small site on a few servers this is good practice, not a necessity.

**Where**
- `internal/metrics/metrics.go`: the four metrics and the middleware
- `internal/server/server.go`: the `/healthz` and `/metrics` routes
- `lb/haproxy.cfg`: the health check

**Run it**

```sh
make up && make seed
make demo-metrics     # a short load, then GET /metrics and GET /healthz as printed
```
