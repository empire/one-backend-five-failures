# Step 4: a queue and a worker for work that can wait

**The failure.** Every open calls the analytics API and waits for it; every create waited for the safety scan. When that API is slow, your users wait for a call they do not care about, and when it is down, the calls fail on the user's request.

**The part.** The app writes one small event and answers. A worker reads the events and makes the calls.
- Redis Streams in the state store, **one stream for each type of job** (`events:click`, `events:scan`), each with its own workers, so an outage at one provider does not stop the other.
- An event leaves the stream only after the call worked. A failed call is tried again: the wait doubles each time (100 ms up to 2 s) and a random part is added, so many retries do not land at the same moment. A worker that dies leaves its event in the stream, and another worker takes it over after 30 seconds.
- The queue can deliver an event twice, so each event has an ID and the worker drops an ID it has already seen. The ID also goes to the provider in `X-Event-ID`.
- A create now saves the link and queues the scan; if the scan finds the URL unsafe, the worker deletes the link. A link can be opened in the moment before its scan is done.

**Where**
- `internal/queue/queue.go`: `Producer`, `Consumer` (retry, takeover, dedupe) and `Backoff`
- `cmd/worker/main.go`: the two lanes and what each does
- `internal/server/server.go`: `create` and `open` publish events instead of calling the APIs

**Not built:** a dead-letter lane (an event is retried forever), and a limit on the size of a lane.

**Run it**

```sh
make up && make seed
make demo-outage            # analytics fails every call for 20 s: events wait, scans carry on, the backlog drains
make demo-dupes             # 20 events added twice each: the worker drops the repeats
make up DEDUPE=off && make demo-dupes NAME=dupes-off   # and what the provider sees without that
make slow-open              # one open while analytics is slow (run it on step 3 too, to compare)
```

The demos write `results/step-4-queue/<name>/run-1/demo.json`.
