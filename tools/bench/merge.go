package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type runSummary struct {
	KneeRPS float64            `json:"knee_rps"`
	P95     float64            `json:"p95_at_knee_ms"`
	Fail2x  float64            `json:"fail_pct_at_2x_knee"`
	CPU     map[string]float64 `json:"cpu_at_knee"`
}

// merge takes the median of several ramp runs and keeps the per-second series of the median run.
func merge(args []string) {
	fs := flag.NewFlagSet("merge", flag.ExitOnError)
	out := fs.String("out", ".", "directory for the merged series.json and summary.json")
	_ = fs.Parse(args)
	dirs := fs.Args()
	if len(dirs) == 0 {
		log.Fatal("usage: bench merge -out dir run-1 run-2 ...")
	}

	type run struct {
		dir string
		s   runSummary
	}
	var runs []run
	for _, d := range dirs {
		b, err := os.ReadFile(filepath.Join(d, "summary.json"))
		if err != nil {
			log.Fatal(err)
		}
		var s runSummary
		if err := json.Unmarshal(b, &s); err != nil {
			log.Fatal(err)
		}
		runs = append(runs, run{d, s})
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].s.KneeRPS < runs[j].s.KneeRPS })
	mid := runs[(len(runs)-1)/2]

	var knees, p95s, fails []float64
	cpu := map[string][]float64{}
	for _, r := range runs {
		knees = append(knees, r.s.KneeRPS)
		p95s = append(p95s, r.s.P95)
		fails = append(fails, r.s.Fail2x)
		for k, v := range r.s.CPU {
			cpu[k] = append(cpu[k], v)
		}
	}
	cpuMed := map[string]float64{}
	for k, v := range cpu {
		cpuMed[k] = median(v)
	}
	summary := map[string]any{
		"runs":                len(runs),
		"knee_rps":            median(knees),
		"knee_rps_all_runs":   knees,
		"p95_at_knee_ms":      median(p95s),
		"fail_pct_at_2x_knee": median(fails),
		"cpu_at_knee":         cpuMed,
		"series_from":         mid.dir,
	}
	if b, err := os.ReadFile(filepath.Join(mid.dir, "series.json")); err == nil {
		_ = os.WriteFile(filepath.Join(*out, "series.json"), b, 0o644)
	}
	writeJSON(filepath.Join(*out, "summary.json"), summary)
}

func median(v []float64) float64 {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	return s[(len(s)-1)/2]
}

// envInfo records where a result was measured.
func envInfo(args []string) {
	fs := flag.NewFlagSet("env", flag.ExitOnError)
	out := fs.String("out", "env.json", "output file")
	_ = fs.Parse(args)
	cpuModel := ""
	if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(l, "model name") {
				cpuModel = strings.TrimSpace(l[strings.Index(l, ":")+1:])
				break
			}
		}
	}
	run := func(name string, a ...string) string {
		b, _ := exec.Command(name, a...).Output()
		return strings.TrimSpace(string(b))
	}
	writeJSON(*out, map[string]any{
		"cpu_model": cpuModel,
		"cpu_cores": runtime.NumCPU(),
		"docker":    run("docker", "version", "--format", "{{.Server.Version}}"),
		"go":        runtime.Version(),
		"commit":    run("git", "rev-parse", "--short", "HEAD"),
		"dirty":     run("git", "status", "--porcelain", "--", ".", ":!results") != "",
		"k6_image":  "grafana/k6:" + run("docker", "image", "inspect", "grafana/k6", "--format", "{{index .Config.Labels \"org.opencontainers.image.version\"}}"),
	})
}
