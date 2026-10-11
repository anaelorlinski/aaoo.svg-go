#!/bin/sh
# Fetch the resvg regression suite (1740 SVG/PNG pairs) into testdata/.
#
# Blobless + sparse: only the test subtree's objects are downloaded, not
# resvg's full history (~25 MB) or its Rust sources and build artifacts.
#
# resvg is dual-licensed Apache-2.0 / MIT; the fetched tree keeps its own
# LICENSE files. Nothing here is redistributed — testdata/ is gitignored.
set -eu

REPO=https://github.com/linebender/resvg
# Pin the suite. Bump deliberately, and re-baseline when you do.
COMMIT=${RESVG_COMMIT:-main}
DEST=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/testdata/resvg

if [ -d "$DEST/.git" ]; then
	echo "already present: $DEST"
	exit 0
fi

mkdir -p "$(dirname "$DEST")"
git clone --filter=blob:none --sparse "$REPO" "$DEST"
git -C "$DEST" sparse-checkout set crates/resvg/tests
git -C "$DEST" checkout "$COMMIT"

echo "suite ready: $DEST/crates/resvg/tests"
