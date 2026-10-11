package svgtests

import (
	"encoding/csv"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// WidthFromReference tells [Run] to render each case at its own reference
// PNG's width, which is what keeps this harness self-contained.
//
// The pre-existing Rust harness instead re-rendered every reference with
// resvg at a uniform 400px (crates usvg + fontdb). That is more uniform but
// needs a Rust toolchain and a resvg build; using the suite's checked-in
// references costs comparability with those older numbers and buys a harness
// that runs with nothing but Go. Re-baseline rather than compare across the
// two.
const WidthFromReference = 0

// Case is one suite entry: an SVG and the reference PNG beside it.
type Case struct {
	Name string // slash-separated, relative to the tests root, no extension
	SVG  string
	PNG  string
}

// FontsDir returns the suite's own font directory, which sits beside its
// tests. The text cases name specific families, so a renderer that does not
// load these is not being measured on text support at all.
func FontsDir(root string) string {
	for _, c := range []string{
		filepath.Join(root, "crates", "resvg", "tests", "fonts"),
		filepath.Join(root, "fonts"),
		filepath.Join(filepath.Dir(root), "fonts"),
	} {
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			return c
		}
	}
	return ""
}

// LoadSuite walks a resvg checkout and pairs each .svg with the .png next to
// it. root is the directory holding "tests", i.e. crates/resvg/tests — or any
// ancestor of it, which is resolved.
func LoadSuite(root string) ([]Case, error) {
	tests := root
	if fi, err := os.Stat(filepath.Join(root, "crates", "resvg", "tests", "tests")); err == nil && fi.IsDir() {
		tests = filepath.Join(root, "crates", "resvg", "tests", "tests")
	} else if fi, err := os.Stat(filepath.Join(root, "tests")); err == nil && fi.IsDir() {
		tests = filepath.Join(root, "tests")
	}

	var out []Case
	err := filepath.WalkDir(tests, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".svg") {
			return err
		}
		png := strings.TrimSuffix(p, ".svg") + ".png"
		if _, err := os.Stat(png); err != nil {
			return nil // no reference: not a comparison test
		}
		rel, err := filepath.Rel(tests, strings.TrimSuffix(p, ".svg"))
		if err != nil {
			return err
		}
		out = append(out, Case{Name: filepath.ToSlash(rel), SVG: p, PNG: png})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no test cases under %s (run ./fetch-suite.sh?)", tests)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Renderer renders SVG source to an image. Warnings are the renderer's own
// diagnostics; a test that renders wrongly *without* warning is the failure
// mode this suite exists to surface, so they are recorded per case.
//
// This is the seam that lets the harness outlive its current backend: today
// it is svgrender, on the draw engine.
type Renderer interface {
	Render(svg []byte, width int) (img image.Image, warnings []string, err error)
}

// Result is one case's outcome.
type Result struct {
	Case
	Diff
	Warnings []string
	Err      error
}

// Silent reports whether the case looks wrong while the renderer said nothing.
// Shrinking this population is the point of putting SVG on the property model.
func (r Result) Silent() bool {
	return r.Err == nil && r.Bucket() == Bad && len(r.Warnings) == 0
}

// Run renders and compares every case. Cases are independent; failures are
// recorded, never fatal.
func Run(cases []Case, r Renderer, width int) []Result {
	out := make([]Result, 0, len(cases))
	for _, c := range cases {
		out = append(out, runOne(c, r, width))
	}
	return out
}

func runOne(c Case, r Renderer, width int) Result {
	res := Result{Case: c}

	src, err := os.ReadFile(c.SVG)
	if err != nil {
		res.Err = err
		return res
	}
	ref, err := loadPNG(c.PNG)
	if err != nil {
		res.Err = fmt.Errorf("reference: %w", err)
		return res
	}

	if width <= 0 {
		width = ref.Bounds().Dx()
	}
	img, warns, err := r.Render(src, width)
	res.Warnings = warns
	if err != nil {
		res.Err = err
		// still record the reference size, so the row is comparable
		b := ref.Bounds()
		res.Diff = Diff{RefW: b.Dx(), RefH: b.Dy(), TotalPx: int64(b.Dx()) * int64(b.Dy())}
		res.Diff.DiffT1, res.Diff.DiffT16 = res.Diff.TotalPx, res.Diff.TotalPx
		res.Diff.MaxDelta = 255
		return res
	}
	res.Diff = Compare(ref, img)
	return res
}

func loadPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

// WriteCSV emits one row per case, column-compatible with the pre-existing
// Rust harness so old and new runs can be diffed directly.
func WriteCSV(w io.Writer, results []Result) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{
		"test", "bucket", "ref_w", "ref_h", "out_w", "out_h", "total_px",
		"diff_t1", "diff_t16", "pct_t1", "pct_t16", "max_delta", "silent",
		"warnings", "run_error",
	}); err != nil {
		return err
	}
	for _, r := range results {
		errStr := ""
		if r.Err != nil {
			errStr = r.Err.Error()
		}
		if err := cw.Write([]string{
			r.Name, string(r.Bucket()),
			itoa(r.RefW), itoa(r.RefH), itoa(r.OutW), itoa(r.OutH),
			strconv.FormatInt(r.TotalPx, 10),
			strconv.FormatInt(r.DiffT1, 10), strconv.FormatInt(r.DiffT16, 10),
			fmt.Sprintf("%.3f", r.PctT1()), fmt.Sprintf("%.3f", r.PctT16()),
			itoa(r.MaxDelta), strconv.FormatBool(r.Silent()),
			strings.Join(r.Warnings, " | "), errStr,
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func itoa(v int) string { return strconv.Itoa(v) }

// Summary counts results by bucket, by area, and — the metric that matters
// while SVG still parses values as strings — how many fail silently.
type Summary struct {
	Total   int
	Buckets map[Bucket]int
	Silent  int
	Errors  int
	ByArea  map[string]map[Bucket]int
}

func Summarize(results []Result) Summary {
	s := Summary{
		Total:   len(results),
		Buckets: map[Bucket]int{},
		ByArea:  map[string]map[Bucket]int{},
	}
	for _, r := range results {
		b := r.Bucket()
		s.Buckets[b]++
		if r.Silent() {
			s.Silent++
		}
		if r.Err != nil {
			s.Errors++
		}
		area, _, _ := strings.Cut(r.Name, "/")
		if s.ByArea[area] == nil {
			s.ByArea[area] = map[Bucket]int{}
		}
		s.ByArea[area][b]++
	}
	return s
}

func (s Summary) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d tests\n\n", s.Total)
	for _, k := range []Bucket{Perfect, Good, Close, Bad} {
		fmt.Fprintf(&b, "  %-8s %5d  (%4.1f%%)\n", k, s.Buckets[k], pct(int64(s.Buckets[k]), int64(s.Total)))
	}
	fmt.Fprintf(&b, "\n  silently wrong (bad, no warning): %d\n", s.Silent)
	if s.Errors > 0 {
		fmt.Fprintf(&b, "  render errors:                    %d\n", s.Errors)
	}

	areas := make([]string, 0, len(s.ByArea))
	for a := range s.ByArea {
		areas = append(areas, a)
	}
	sort.Strings(areas)
	fmt.Fprintf(&b, "\n%-18s %7s %7s %7s %7s %7s\n", "area", "total", "perfect", "good", "close", "bad")
	for _, a := range areas {
		m := s.ByArea[a]
		tot := m[Perfect] + m[Good] + m[Close] + m[Bad]
		fmt.Fprintf(&b, "%-18s %7d %7d %7d %7d %7d\n", a, tot, m[Perfect], m[Good], m[Close], m[Bad])
	}
	return b.String()
}
