# svgtests

Conformance harness: runs the [resvg](https://github.com/linebender/resvg)
regression suite (1722 cases) against this repo's SVG renderer and reports
per-test pixel differences.

It lives in the `svgrender` module, beside the renderer it measures: the
SVG library itself must not depend on a rasterising backend, and
`svgrender` already does. Only `canvasrenderer` knows the renderer.

## Use

```sh
./fetch-suite.sh                  # once — fetches the suite into testdata/
go run ./cmd/svgconf              # writes out/results.csv, prints a summary
go run ./cmd/svgconf -filter shapes/rect     # a subset
go test ./...                     # regression check against baseline.csv
```

`RESVG_SUITE=/path/to/resvg` reuses an existing checkout instead of fetching.
`go test` **skips** cleanly when the suite is absent, so a fresh clone builds
and tests without it.

The fetch is a blobless sparse clone of the test subtree only — not resvg's
full history (~25 MB) or its Rust sources. `testdata/` and `out/` are
gitignored; nothing from resvg is redistributed here. resvg is dual-licensed
Apache-2.0 / MIT.

## What it measures

Buckets follow the pre-existing Rust harness so the shape of the numbers is
familiar: `perfect` (no pixel differs by more than 1 per channel), `good`
(≤0.1% differ by more than 16), `close` (≤1%), `bad` (the rest, or a size
mismatch).

**The metric that matters is not the pass rate — it is `silently wrong`:**
cases that render incorrectly while the renderer emits no warning at all.
That population exists because `svg` parses CSS values as strings rather than
through the CSS engine, so a value it cannot understand becomes `0` instead of
an error. Collapsing it is the goal of putting SVG on the property model; see
`../docs/svg-separation.md` T9.

## References encode resvg's behaviour, not the spec

The checked-in PNGs are resvg's own output, **including its gaps**. A test can
therefore "regress" because this renderer became *more* correct — the first
run of T9 hit exactly that: `shapes/rect/rem-values` went perfect -> bad
because resvg draws nothing there (`rem` is unimplemented in it) while we now
resolve `rem` against the root font size, as SVG 2 requires.

So read a regression as a prompt to investigate, never as a verdict. The
`silently wrong` count is the robust metric: it measures whether the renderer
*knows* what it could not do, which is true regardless of what resvg supports.

## Baseline

`baseline.csv` is a committed run. `go test` fails on **regressions** and on
growth in the silent bucket, never on the absolute rate. Re-baseline
deliberately:

```sh
go run ./cmd/svgconf && cp out/results.csv baseline.csv
```

The baseline is tied to the pinned resvg commit in `fetch-suite.sh`; bump both
together.

Known deviations, where the colour parser follows CSS Color 4 and the resvg
reference does not (rated `bad`, deliberately):

- `painting/fill/hsl-120-200percent-25percent`: the reference clamps the
  saturation to 100% (CSS 3); CSS Color 4 only clamps negative saturation,
  and the out-of-gamut result is gamut-mapped.
- `painting/fill/rgba-0-127-0-50percent`: the reference rejects a
  percentage alpha in the legacy comma syntax; CSS Color 4 accepts it.

## Differences from the earlier Rust harness

`AO/webrender-svg-test/testbed` re-rendered every reference with resvg at a
uniform 400px (crates `usvg` + `fontdb`). This harness uses the suite's
**checked-in** reference PNGs at their own size (1698 are 300×300, 20 are
300×150, 4 are 500×500), which costs comparability with those older numbers
and buys a harness needing nothing but Go. The two agree closely anyway —
365/121/133/1103 here against 366/129/155/1067 there.
