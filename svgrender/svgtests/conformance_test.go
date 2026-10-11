package svgtests_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anaelorlinski/aaoo.svg-go/svgrender/svgtests"
	"github.com/anaelorlinski/aaoo.svg-go/svgrender/svgtests/canvasrenderer"
)

// suiteDir locates the resvg suite, or "" if it has not been fetched.
// RESVG_SUITE overrides, so an existing checkout can be reused.
func suiteDir() string {
	if d := os.Getenv("RESVG_SUITE"); d != "" {
		return d
	}
	d := filepath.Join("testdata", "resvg")
	if _, err := os.Stat(d); err == nil {
		return d
	}
	return ""
}

// TestConformance runs the full suite and fails only on a *regression*
// against baseline.csv — never on the absolute pass rate, which is expected
// to be poor until SVG stops parsing values as strings.
//
// Skips cleanly when the suite is absent, so `go test ./...` is safe on a
// fresh checkout.
func TestConformance(t *testing.T) {
	dir := suiteDir()
	if dir == "" {
		t.Skip("resvg suite not present: run ./fetch-suite.sh (or set RESVG_SUITE)")
	}
	if testing.Short() {
		t.Skip("conformance run takes ~10s")
	}

	cases, err := svgtests.LoadSuite(dir)
	if err != nil {
		t.Fatal(err)
	}
	results := svgtests.Run(cases, canvasrenderer.New(svgtests.FontsDir(dir)), svgtests.WidthFromReference)

	s := svgtests.Summarize(results)
	t.Logf("\n%s", s)

	if _, err := os.Stat("baseline.csv"); err != nil {
		t.Skipf("no baseline.csv to compare against (%v); "+
			"write one with: go run ./cmd/svgconf && cp out/results.csv baseline.csv", err)
	}
	diff, err := svgtests.CompareRuns("baseline.csv", results)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Regressed) > 0 {
		t.Errorf("%d tests regressed against baseline.csv:%s", len(diff.Regressed), diff)
	}
	if diff.Silent > diff.SilentBefore {
		t.Errorf("silent failures grew: %d -> %d", diff.SilentBefore, diff.Silent)
	}
	if len(diff.Improved) > 0 {
		t.Logf("%d tests improved — re-baseline when you are happy with them", len(diff.Improved))
	}
}
