// Command svgconf runs the resvg regression suite against this repo's SVG
// renderer and writes results.csv plus a summary.
//
//	./fetch-suite.sh              # once, into testdata/ (gitignored)
//	go run ./cmd/svgconf          # writes out/results.csv, prints the summary
//	go run ./cmd/svgconf -baseline out/results.csv   # compare against a run
//
// The renderer is injected, so this harness outlives its current backend:
// today svgrender, on the draw engine.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/anaelorlinski/aaoo.svg-go/svgrender/svgtests"
	"github.com/anaelorlinski/aaoo.svg-go/svgrender/svgtests/canvasrenderer"
)

func main() {
	suite := flag.String("suite", "testdata/resvg", "resvg checkout holding the test suite")
	outDir := flag.String("out", "out", "directory for results.csv")
	width := flag.Int("width", svgtests.WidthFromReference, "render width in px; 0 = each reference's own width")
	filter := flag.String("filter", "", "only run tests whose name contains this")
	baseline := flag.String("baseline", "", "results.csv to compare against")
	dump := flag.String("dump", "", "directory to write the rendered images to, beside a copy of their references")
	flag.Parse()

	cases, err := svgtests.LoadSuite(*suite)
	if err != nil {
		log.Fatal(err)
	}
	if *filter != "" {
		var kept []svgtests.Case
		for _, c := range cases {
			if strings.Contains(c.Name, *filter) {
				kept = append(kept, c)
			}
		}
		cases = kept
	}
	fmt.Fprintf(os.Stderr, "running %d tests...\n", len(cases))

	var each func(svgtests.Case, image.Image)
	if *dump != "" {
		each = func(c svgtests.Case, img image.Image) {
			if err := dumpCase(*dump, c, img); err != nil {
				log.Print(err)
			}
		}
	}
	results := svgtests.RunEach(cases, canvasrenderer.New(svgtests.FontsDir(*suite)), *width, each)

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatal(err)
	}
	csvPath := filepath.Join(*outDir, "results.csv")
	f, err := os.Create(csvPath)
	if err != nil {
		log.Fatal(err)
	}
	if err := svgtests.WriteCSV(f, results); err != nil {
		log.Fatal(err)
	}
	f.Close()

	fmt.Print(svgtests.Summarize(results))
	fmt.Fprintf(os.Stderr, "\nwrote %s\n", csvPath)

	if *baseline != "" {
		diff, err := svgtests.CompareRuns(*baseline, results)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Print(diff)
	}
}

// dumpCase writes the image of a case as <name>.out.png, with a copy of its
// reference as <name>.ref.png, in one flat directory.
func dumpCase(dir string, c svgtests.Case, img image.Image) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	base := filepath.Join(dir, strings.ReplaceAll(c.Name, "/", "__"))
	f, err := os.Create(base + ".out.png")
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	ref, err := os.ReadFile(c.PNG)
	if err != nil {
		return err
	}
	return os.WriteFile(base+".ref.png", ref, 0o644)
}
