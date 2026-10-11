package svgtests

import (
	"encoding/csv"
	"fmt"
	"os"
	"sort"
	"strings"
)

// RunDiff is the change between a baseline run and the current one.
type RunDiff struct {
	Improved, Regressed  []Move
	SilentBefore, Silent int
	NewTests, GoneTests  []string
}

// Move is one test whose bucket changed.
type Move struct {
	Name     string
	From, To Bucket
}

var rank = map[Bucket]int{Bad: 0, Close: 1, Good: 2, Perfect: 3}

// CompareRuns reads a previous results.csv and reports what moved. Column
// layout is shared with the pre-existing Rust harness, so its runs can be
// used as a baseline directly.
func CompareRuns(baselinePath string, current []Result) (RunDiff, error) {
	prev, prevSilent, err := readBuckets(baselinePath)
	if err != nil {
		return RunDiff{}, err
	}

	d := RunDiff{SilentBefore: prevSilent}
	seen := map[string]bool{}
	for _, r := range current {
		seen[r.Name] = true
		if r.Silent() {
			d.Silent++
		}
		was, ok := prev[r.Name]
		if !ok {
			d.NewTests = append(d.NewTests, r.Name)
			continue
		}
		now := r.Bucket()
		switch {
		case rank[now] > rank[was]:
			d.Improved = append(d.Improved, Move{r.Name, was, now})
		case rank[now] < rank[was]:
			d.Regressed = append(d.Regressed, Move{r.Name, was, now})
		}
	}
	for name := range prev {
		if !seen[name] {
			d.GoneTests = append(d.GoneTests, name)
		}
	}
	sort.Strings(d.NewTests)
	sort.Strings(d.GoneTests)
	sort.Slice(d.Regressed, func(i, j int) bool { return d.Regressed[i].Name < d.Regressed[j].Name })
	sort.Slice(d.Improved, func(i, j int) bool { return d.Improved[i].Name < d.Improved[j].Name })
	return d, nil
}

// readBuckets tolerates both this harness's CSV and the older Rust one: it
// locates columns by name rather than position.
func readBuckets(path string) (map[string]Bucket, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	rows, err := csv.NewReader(f).ReadAll()
	if err != nil || len(rows) == 0 {
		return nil, 0, fmt.Errorf("reading %s: %w", path, err)
	}
	col := map[string]int{}
	for i, name := range rows[0] {
		col[name] = i
	}
	ti, bi := col["test"], col["bucket"]
	si, hasSilent := col["silent"]

	out := make(map[string]Bucket, len(rows))
	silent := 0
	for _, r := range rows[1:] {
		if ti >= len(r) || bi >= len(r) {
			continue
		}
		out[r[ti]] = Bucket(r[bi])
		if hasSilent && si < len(r) && r[si] == "true" {
			silent++
		}
	}
	return out, silent, nil
}

func (d RunDiff) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n=== against baseline ===\n")
	fmt.Fprintf(&b, "  improved:  %d\n", len(d.Improved))
	fmt.Fprintf(&b, "  regressed: %d\n", len(d.Regressed))
	if d.SilentBefore > 0 || d.Silent > 0 {
		fmt.Fprintf(&b, "  silently wrong: %d -> %d\n", d.SilentBefore, d.Silent)
	}
	if n := len(d.NewTests); n > 0 {
		fmt.Fprintf(&b, "  new tests: %d\n", n)
	}
	if n := len(d.GoneTests); n > 0 {
		fmt.Fprintf(&b, "  missing from this run: %d\n", n)
	}
	// Regressions are what block a change; list them all.
	for _, m := range d.Regressed {
		fmt.Fprintf(&b, "  REGRESSED %s: %s -> %s\n", m.Name, m.From, m.To)
	}
	return b.String()
}
