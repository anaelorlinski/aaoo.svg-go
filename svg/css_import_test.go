package svg

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anaelorlinski/aaoo.cssengine-go/limits"
	"github.com/anaelorlinski/aaoo.svg-go/internal/utils"
	"golang.org/x/net/html"
)

// findRect returns the first <rect> of the cascaded tree.
func findRect(n *cascadedNode) *cascadedNode {
	if n == nil {
		return nil
	}
	if n.tag == "rect" {
		return n
	}
	for _, c := range n.children {
		if r := findRect(c); r != nil {
			return r
		}
	}
	return nil
}

func importContext(t *testing.T, svgSource, baseURL string, fetcher Fetcher) *svgContext {
	t.Helper()
	root, err := html.Parse(strings.NewReader(svgSource))
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := newSVGContext(root, baseURL, fetcher, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

// SVG1: @import must go through the caller's fetcher, never DefaultUrlFetcher.
func TestImportUsesCallerFetcher(t *testing.T) {
	dir := t.TempDir()
	cssPath := filepath.Join(dir, "evil.css")
	if err := os.WriteFile(cssPath, []byte("rect { data-leak: from-disk }"), 0o644); err != nil {
		t.Fatal(err)
	}
	cssURL, err := utils.PathToURL(cssPath)
	if err != nil {
		t.Fatal(err)
	}
	svgSource := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg"><style>@import url(%q);</style><rect width="1" height="1"/></svg>`, cssURL)

	// A recording fetcher that refuses everything: nothing must be read from disk.
	var calls []string
	refuse := func(url string) (Resource, error) {
		calls = append(calls, url)
		return Resource{}, fmt.Errorf("refused: %s", url)
	}
	ctx := importContext(t, svgSource, "", refuse)
	if len(calls) != 1 || calls[0] != cssURL {
		t.Fatalf("expected one fetch of %s via the caller fetcher, got %v", cssURL, calls)
	}
	if rect := findRect(ctx.root); rect == nil || rect.attrs["data-leak"] != "" {
		t.Fatalf("stylesheet was read without the caller fetcher: %v", rect)
	}

	// A fetcher serving its own content: that content is what gets applied.
	serve := func(url string) (Resource, error) {
		return Resource{Content: bytes.NewReader([]byte("rect { data-leak: from-fetcher }")), MimeType: "text/css"}, nil
	}
	ctx = importContext(t, svgSource, "", serve)
	if rect := findRect(ctx.root); rect == nil || rect.attrs["data-leak"] != "from-fetcher" {
		t.Fatalf("expected the caller fetcher's stylesheet to apply, got %v", rect)
	}

	// No fetcher: imports are skipped, not fetched with a default.
	ctx = importContext(t, svgSource, "", nil)
	if rect := findRect(ctx.root); rect == nil || rect.attrs["data-leak"] != "" {
		t.Fatalf("nil fetcher must not fall back to reading the stylesheet: %v", rect)
	}
}

// SVG2: a self-importing (or cyclic) stylesheet must terminate with a bounded
// number of fetches.
func TestImportCycleTerminates(t *testing.T) {
	var calls int
	fetcher := func(url string) (Resource, error) {
		calls++
		if calls > 100 {
			panic("unbounded @import recursion")
		}
		switch url {
		case "https://example.com/a.css":
			return Resource{Content: bytes.NewReader([]byte(`@import "b.css"; rect { data-a: yes }`))}, nil
		case "https://example.com/b.css":
			return Resource{Content: bytes.NewReader([]byte(`@import "a.css"; @import "b.css"; rect { data-b: yes }`))}, nil
		}
		return Resource{}, fmt.Errorf("not found: %s", url)
	}
	svgSource := `<svg xmlns="http://www.w3.org/2000/svg"><style>@import "a.css";</style><rect width="1" height="1"/></svg>`
	ctx := importContext(t, svgSource, "https://example.com/doc.svg", fetcher)
	if calls != 2 {
		t.Fatalf("expected 2 fetches (a.css, b.css), got %d", calls)
	}
	rect := findRect(ctx.root)
	if rect == nil || rect.attrs["data-a"] != "yes" || rect.attrs["data-b"] != "yes" {
		t.Fatalf("imported rules not applied: %v", rect)
	}
}

// SVG2: a non-cyclic but deep chain of distinct imports stops at the depth limit.
func TestImportDepthLimit(t *testing.T) {
	var calls int
	fetcher := func(url string) (Resource, error) {
		calls++
		var n int
		fmt.Sscanf(url, "https://example.com/%d.css", &n)
		return Resource{Content: bytes.NewReader([]byte(fmt.Sprintf(`@import "%d.css";`, n+1)))}, nil
	}
	svgSource := `<svg xmlns="http://www.w3.org/2000/svg"><style>@import "0.css";</style><rect/></svg>`
	importContext(t, svgSource, "https://example.com/doc.svg", fetcher)
	if calls != limits.ImportDepth {
		t.Fatalf("expected %d fetches, got %d", limits.ImportDepth, calls)
	}
}
