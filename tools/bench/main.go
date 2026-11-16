// Command bench turns one load-test run into numbers.
//
//	bench sample -out cpu.jsonl -services app1,app2,db  record container CPU (and /debug/vars) once a second until stopped
//	bench series -k6 k6.json.gz -cpu cpu.jsonl ...   aggregate one k6 run into series.json and summary.json
//	bench demo -k6 k6.json.gz -out dir               score a steady run: 401 share, and the effect of a kill
//
// CPU is read from the container's cgroup, as a share of the container's own CPU cap,
// so 100 means "this container is using all the CPU it is allowed".
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: bench sample|series [flags]")
	}
	switch os.Args[1] {
	case "sample":
		sample(os.Args[2:])
	case "series":
		series(os.Args[2:])
	case "demo":
		demo(os.Args[2:])
	default:
		log.Fatalf("unknown command %q", os.Args[1])
	}
}

// ---------------------------------------------------------------- sample

type sampleLine struct {
	T      int64              `json:"t"` // unix milliseconds
	CPU    map[string]float64 `json:"cpu"`
	Expvar map[string]float64 `json:"expvar,omitempty"`
}

type cgroup struct {
	dir  string
	prev int64
}

func sample(args []string) {
	fs := flag.NewFlagSet("sample", flag.ExitOnError)
	out := fs.String("out", "cpu.jsonl", "output file")
	services := fs.String("services", "app,db", "compose services to watch (comma separated)")
	project := fs.String("project", "shortener", "compose project name")
	expvarURL := fs.String("expvar", "", "optional /debug/vars URL(s), comma separated; numeric values are recorded and summed over the URLs")
	_ = fs.Parse(args)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	f, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()

	groups := map[string][]*cgroup{}
	for _, svc := range strings.Split(*services, ",") {
		for _, id := range containerIDs(*project, svc) {
			groups[svc] = append(groups[svc], &cgroup{dir: "/sys/fs/cgroup/system.slice/docker-" + id + ".scope"})
		}
		if len(groups[svc]) == 0 {
			log.Fatalf("no running container for service %q", svc)
		}
	}

	last := time.Now()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			dt := now.Sub(last).Seconds()
			last = now
			line := sampleLine{T: now.UnixMilli(), CPU: map[string]float64{}}
			for svc, cgs := range groups {
				var sum float64
				var n int
				for _, cg := range cgs {
					if pct, ok := cg.percent(dt); ok {
						sum += pct
						n++
					}
				}
				if n > 0 {
					line.CPU[svc] = math.Round(sum/float64(n)*10) / 10
				}
			}
			if *expvarURL != "" {
				line.Expvar = fetchExpvar(*expvarURL)
			}
			b, _ := json.Marshal(line)
			w.Write(b)
			w.WriteByte('\n')
			w.Flush()
		}
	}
}

func containerIDs(project, service string) []string {
	out, err := exec.Command("docker", "ps", "-q", "--no-trunc",
		"--filter", "label=com.docker.compose.project="+project,
		"--filter", "label=com.docker.compose.service="+service).Output()
	if err != nil {
		log.Fatalf("docker ps: %v", err)
	}
	return strings.Fields(string(out))
}

// percent returns CPU use over the last dt seconds as a share of this container's CPU cap.
func (c *cgroup) percent(dt float64) (float64, bool) {
	usage, ok := readKey(filepath.Join(c.dir, "cpu.stat"), "usage_usec")
	if !ok {
		return 0, false
	}
	prev := c.prev
	c.prev = usage
	if prev == 0 {
		return 0, false
	}
	capCPUs := float64(runtime.NumCPU())
	if b, err := os.ReadFile(filepath.Join(c.dir, "cpu.max")); err == nil {
		if p := strings.Fields(string(b)); len(p) == 2 && p[0] != "max" {
			q, _ := strconv.ParseFloat(p[0], 64)
			per, _ := strconv.ParseFloat(p[1], 64)
			if per > 0 {
				capCPUs = q / per
			}
		}
	}
	return float64(usage-prev) / 1e6 / dt / capCPUs * 100, true
}

func readKey(path, key string) (int64, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	for _, l := range strings.Split(string(b), "\n") {
		if p := strings.Fields(l); len(p) == 2 && p[0] == key {
			n, err := strconv.ParseInt(p[1], 10, 64)
			return n, err == nil
		}
	}
	return 0, false
}

// fetchExpvar sums the numeric values of every URL. If one URL cannot be read, it returns nil:
// a sum over fewer apps would look like a drop in the counter.
func fetchExpvar(urls string) map[string]float64 {
	total := map[string]float64{}
	for _, u := range strings.Split(urls, ",") {
		one := fetchExpvarOne(u)
		if one == nil {
			return nil
		}
		for k, v := range one {
			total[k] += v
		}
	}
	return total
}

func fetchExpvarOne(url string) map[string]float64 {
	c := http.Client{Timeout: 800 * time.Millisecond}
	resp, err := c.Get(url)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var all map[string]json.RawMessage
	if json.NewDecoder(resp.Body).Decode(&all) != nil {
		return nil
	}
	out := map[string]float64{}
	for k, v := range all {
		var n float64
		if json.Unmarshal(v, &n) == nil {
			out[k] = n
		}
	}
	return out
}

// ---------------------------------------------------------------- series

type k6Point struct {
	Type   string `json:"type"`
	Metric string `json:"metric"`
	Data   struct {
		Time  time.Time         `json:"time"`
		Value float64           `json:"value"`
		Tags  map[string]string `json:"tags"`
	} `json:"data"`
}

type bucket struct {
	durations                              []float64
	ok, unauth, throttled, failed, dropped int
	creates                                int // requests of kind "create" (the ones that need a session)
	// The one abuser (scenario "abuser") is counted apart from the normal clients.
	abuser, abuserOK, abuserThrottled, abuserFailed int
	// The same requests by kind and outcome (a 2xx or 3xx answer is "ok"; a 429 from the limiter is in neither; anything else, a refused connection or a timeout included, is "failed").
	openOK, openFailed, createOK, createFailed int
}

// SecondRow is one second of one run, as the video reads it.
type SecondRow struct {
	T         int `json:"t"`
	Level     int `json:"level"`
	Requests  int `json:"requests"`
	Creates   int `json:"creates"`
	OK        int `json:"ok"`
	Unauth    int `json:"unauth"`
	Throttled int `json:"throttled"`
	Failed    int `json:"failed"`
	Dropped   int `json:"dropped"`
	// Opens and creates counted apart, by outcome (the replica demos need to say which kind of request fails). A 429 is in neither.
	OpenOK       int                `json:"open_ok,omitempty"`
	OpenFailed   int                `json:"open_failed,omitempty"`
	CreateOK     int                `json:"create_ok,omitempty"`
	CreateFailed int                `json:"create_failed,omitempty"`
	P50          float64            `json:"p50_ms"`
	P95          float64            `json:"p95_ms"`
	CPU          map[string]float64 `json:"cpu,omitempty"`
	DBQPS        float64            `json:"db_qps,omitempty"`
	// The abuser's creates in this second, when there is one: sent, accepted (201), turned away (429).
	Abuser          int `json:"abuser,omitempty"`
	AbuserOK        int `json:"abuser_ok,omitempty"`
	AbuserThrottled int `json:"abuser_throttled,omitempty"`
	// What each counter in /debug/vars grew by this second (opens, db_lookups, cache_hits, cache_misses), summed over the apps.
	Counters map[string]float64 `json:"counters,omitempty"`
	// Levels that are read as they are, not as growth: how many events wait in each lane of the queue.
	Gauges map[string]float64 `json:"gauges,omitempty"`
}

func series(args []string) {
	fs := flag.NewFlagSet("series", flag.ExitOnError)
	k6path := fs.String("k6", "", "k6 --out json file (.gz allowed)")
	cpuPath := fs.String("cpu", "", "cpu.jsonl from `bench sample`")
	outDir := fs.String("out", ".", "directory for series.json")
	stepS := fs.Int("step-s", 10, "seconds per level (STEP_S)")
	stepRPS := fs.Int("step-rps", 250, "requests per second added per level (STEP_RPS)")
	levelsN := fs.Int("levels", 0, "number of levels in the ramp (LEVELS); seconds after the ramp are dropped")
	_ = fs.Parse(args)

	rows, t0 := readK6(*k6path)
	if *levelsN > 0 && len(rows) > *levelsN**stepS {
		rows = rows[:*levelsN**stepS]
	}
	cpu := readCPU(*cpuPath, t0)

	var seconds []SecondRow
	prevLookups := -1.0
	for s := 0; s < len(rows); s++ {
		b := rows[s]
		r := SecondRow{T: s, Level: s / *stepS + 1, Requests: len(b.durations) + b.dropped, Creates: b.creates, OK: b.ok, Unauth: b.unauth, Throttled: b.throttled, Failed: b.failed, Dropped: b.dropped}
		r.P50, r.P95 = pct(b.durations, 50), pct(b.durations, 95)
		if c, ok := cpu[s]; ok {
			r.CPU = c.cpu
			if v, has := c.expvar["db_lookups"]; has && prevLookups >= 0 {
				r.DBQPS = v - prevLookups
			}
			if v, has := c.expvar["db_lookups"]; has {
				prevLookups = v
			}
		}
		seconds = append(seconds, r)
	}
	writeJSON(filepath.Join(*outDir, "series.json"), seconds)

	// A short table to watch the service fail: for each level, how much of the offered load was not answered well.
	fmt.Printf("%-6s %-9s %s\n", "level", "offered", "not served well (errors, timeouts, 401, dropped)")
	for l := 1; (l-1)**stepS < len(seconds); l++ {
		var total, bad int
		for s := (l - 1) * *stepS; s < l**stepS && s < len(seconds); s++ {
			total += seconds[s].Requests
			bad += seconds[s].Failed + seconds[s].Unauth + seconds[s].Dropped
		}
		if total > 0 {
			fmt.Printf("%-6d %-9d %.1f %%\n", l, l**stepRPS, 100*float64(bad)/float64(total))
		}
	}
}

func readK6(path string) ([]bucket, time.Time) {
	f, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			log.Fatal(err)
		}
		defer gz.Close()
		r = gz
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)

	type ev struct {
		t       time.Time
		dur     float64
		status  int
		create  bool
		abuser  bool
		dropped bool
	}
	var evs []ev
	var t0 time.Time
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.Contains(line, []byte(`"type":"Point"`)) {
			continue
		}
		isDur := bytes.Contains(line, []byte(`"metric":"http_req_duration"`))
		isDrop := bytes.Contains(line, []byte(`"metric":"dropped_iterations"`))
		if !isDur && !isDrop {
			continue
		}
		var p k6Point
		if json.Unmarshal(line, &p) != nil {
			continue
		}
		if isDur {
			if k := p.Data.Tags["kind"]; k != "open" && k != "create" {
				continue
			}
			st, _ := strconv.Atoi(p.Data.Tags["status"])
			evs = append(evs, ev{t: p.Data.Time, dur: p.Data.Value, status: st, create: p.Data.Tags["kind"] == "create", abuser: p.Data.Tags["scenario"] == "abuser"})
			if t0.IsZero() || p.Data.Time.Before(t0) {
				t0 = p.Data.Time
			}
		} else {
			evs = append(evs, ev{t: p.Data.Time, dropped: true})
		}
	}
	if err := sc.Err(); err != nil {
		log.Fatal(err)
	}
	if t0.IsZero() {
		log.Fatal("no open/create requests found in the k6 output")
	}
	var rows []bucket
	for _, e := range evs {
		s := int(e.t.Sub(t0).Seconds())
		if s < 0 {
			s = 0
		}
		for len(rows) <= s {
			rows = append(rows, bucket{})
		}
		b := &rows[s]
		switch {
		case e.dropped:
			b.dropped++
		case e.abuser:
			b.abuser++
			switch {
			case e.status == 201:
				b.abuserOK++
			case e.status == 429:
				b.abuserThrottled++
			default:
				b.abuserFailed++
			}
		default:
			b.durations = append(b.durations, e.dur)
			if e.create {
				b.creates++
			}
			switch good := e.status >= 200 && e.status < 400; {
			case e.status == 429: // the limiter turned it away: neither served nor failed
			case e.create && good:
				b.createOK++
			case e.create:
				b.createFailed++
			case good:
				b.openOK++
			default:
				b.openFailed++
			}
			switch {
			case e.status == 401:
				b.unauth++
			case e.status == 429:
				b.throttled++
			case e.status == 0 || e.status >= 400:
				b.failed++
			default:
				b.ok++
			}
		}
	}
	return rows, t0
}

type cpuRow struct {
	cpu    map[string]float64
	expvar map[string]float64
}

func readCPU(path string, t0 time.Time) map[int]cpuRow {
	out := map[int]cpuRow{}
	if path == "" {
		return out
	}
	f, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var l sampleLine
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		s := int((time.UnixMilli(l.T).Sub(t0)).Seconds())
		if s >= 0 {
			out[s] = cpuRow{cpu: l.CPU, expvar: l.Expvar}
		}
	}
	return out
}

func pct(v []float64, p float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	i := int(math.Ceil(p/100*float64(len(s)))) - 1
	if i < 0 {
		i = 0
	}
	return round1(s[i])
}

func round1(x float64) float64 { return math.Round(x*10) / 10 }

func writeJSON(path string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
}
