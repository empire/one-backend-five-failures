package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// demo scores one steady-rate run: the share of logged-in creates answered 401, and, when an app
// was killed during the run, how many requests failed and how long the errors lasted.
//
//	bench demo -k6 k6.json.gz -out dir [-kill-at-file kill.at -window-s 10]
func demo(args []string) {
	fs := flag.NewFlagSet("demo", flag.ExitOnError)
	k6path := fs.String("k6", "", "k6 --out json file (.gz allowed)")
	outDir := fs.String("out", ".", "directory for demo.json and series.json")
	killFile := fs.String("kill-at-file", "", "file holding the unix-millisecond time of the kill (optional)")
	windowS := fs.Int("window-s", 10, "seconds after the kill that count as the kill window")
	sloMS := fs.Float64("slo-ms", 200, "a second whose p95 is over this is a slow second")
	cpuPath := fs.String("cpu", "", "cpu.jsonl from `bench sample` (optional)")
	_ = fs.Parse(args)

	rows, t0 := readK6(*k6path)
	cpu := readCPU(*cpuPath, t0)

	var seconds []SecondRow
	var requests, creates, unauth, failed, dropped int
	for s, b := range rows {
		r := SecondRow{T: s, Requests: len(b.durations) + b.dropped, Creates: b.creates, OK: b.ok, Unauth: b.unauth, Throttled: b.throttled, Failed: b.failed, Dropped: b.dropped}
		r.P50, r.P95 = pct(b.durations, 50), pct(b.durations, 95)
		if c, ok := cpu[s]; ok {
			r.CPU = c.cpu
		}
		seconds = append(seconds, r)
		requests += r.Requests
		creates += b.creates
		unauth += b.unauth
		failed += b.failed
		dropped += b.dropped
	}

	out := map[string]any{
		"requests": requests,
		"creates":  creates,
		"unauth":   unauth,
		"failed":   failed,  // 5xx, timeouts and refused connections; a 401 is counted in "unauth"
		"dropped":  dropped, // requests the load generator could not start (it ran out of clients): the run is not valid if this is not 0
	}
	if creates > 0 {
		out["unauth_pct_of_creates"] = round1(100 * float64(unauth) / float64(creates))
	}

	if *killFile != "" {
		b, err := os.ReadFile(*killFile)
		if err != nil {
			log.Fatal(err)
		}
		ms, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
		if err != nil {
			log.Fatal(err)
		}
		killS := int(time.UnixMilli(ms).Sub(t0).Seconds())
		bad := func(r SecondRow) bool { // the per-second error rate is 1 % or more
			return r.Requests > 0 && float64(r.Failed+r.Unauth)/float64(r.Requests) >= 0.01
		}
		slow := func(r SecondRow) bool { return r.P95 >= *sloMS }
		// Recovery: the first second after the kill from which every later second is under 1 %.
		recovery := 0
		for s := len(seconds) - 1; s >= killS; s-- {
			if bad(seconds[s]) {
				recovery = s - killS + 1
				break
			}
		}
		// Slow seconds: how long the slow end (p95) stayed over the SLO after the kill, and how bad it got.
		slowEnd, slowSecs, maxP95 := 0, 0, 0.0
		for s := killS; s < len(seconds); s++ {
			if slow(seconds[s]) {
				slowEnd = s - killS + 1
				slowSecs++
			}
			if seconds[s].P95 > maxP95 {
				maxP95 = seconds[s].P95
			}
		}
		var wReq, wFail int
		for s := killS; s < killS+*windowS && s < len(seconds); s++ {
			wReq += seconds[s].Requests
			wFail += seconds[s].Failed + seconds[s].Unauth
		}
		out["kill_at_s"] = killS
		out["kill_window_s"] = *windowS
		out["kill_window_requests"] = wReq
		out["kill_errors"] = wFail
		if wReq > 0 {
			out["kill_window_error_pct"] = round1(100 * float64(wFail) / float64(wReq))
		}
		out["kill_recovery_s"] = recovery
		out["kill_slow_until_s"] = slowEnd // seconds after the kill until the last slow second
		out["kill_slow_seconds"] = slowSecs
		out["kill_max_p95_ms"] = maxP95
	}

	writeJSON(filepath.Join(*outDir, "series.json"), seconds)
	writeJSON(filepath.Join(*outDir, "demo.json"), out)
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
}
