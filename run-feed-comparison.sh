#!/bin/sh
# Load .env then run feed-benchmark.
# Configure feeds in .env (FEED_NAME_N / FEED_URL_N); do not hardcode endpoints here.
set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
if [ -f "$project_dir/.env" ]; then
	set -a
	# shellcheck disable=SC1091
	. "$project_dir/.env"
	set +a
fi

duration=${FEED_COMPARISON_DURATION:-30s}
official_flag=
case "${FEED_INCLUDE_OFFICIAL:-true}" in
	0|false|FALSE|no|NO|off|OFF)
		official_flag=--official=false
		;;
esac

resolve_bin() {
	if [ -x "$project_dir/feed-benchmark" ]; then
		printf '%s\n' "$project_dir/feed-benchmark"
		return 0
	fi
	if [ -x "$project_dir/bin/feed-benchmark" ]; then
		printf '%s\n' "$project_dir/bin/feed-benchmark"
		return 0
	fi
	runtime_dir="$project_dir/.runtime"
	echo "Prebuilt binary not found; downloading the release for this platform..." >&2
	INSTALL_DIR="$runtime_dir" "$project_dir/install.sh"
	printf '%s\n' "$runtime_dir/feed-benchmark"
}

bin=$(resolve_bin)
if [ -n "$official_flag" ]; then
	exec "$bin" --duration "$duration" "$official_flag" "$@"
fi
exec "$bin" --duration "$duration" "$@"
