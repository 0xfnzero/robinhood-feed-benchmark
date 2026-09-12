#!/bin/sh
set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
if [ -f "$project_dir/.env" ]; then
	set -a
	. "$project_dir/.env"
	set +a
fi

duration=${FEED_COMPARISON_DURATION:-30s}
if [ -x "$project_dir/feed-benchmark" ]; then
	exec "$project_dir/feed-benchmark" --duration "$duration" "$@"
fi
if [ -x "$project_dir/bin/feed-benchmark" ]; then
	exec "$project_dir/bin/feed-benchmark" --duration "$duration" "$@"
fi

runtime_dir="$project_dir/.runtime"
echo "Prebuilt binary not found; downloading the release for this platform..." >&2
INSTALL_DIR="$runtime_dir" "$project_dir/install.sh"
exec "$runtime_dir/feed-benchmark" --duration "$duration" "$@"
