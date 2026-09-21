#!/bin/sh
# Load .env, listen for RHF2 binary TCP, and serve Nitro JSON WebSocket feed.
#
# Example .env:
#   FEED_VENDOR_1=RHF2
#   FEED_URL_1=tcp://0.0.0.0:19770
#   NITRO_FEED_PORT=9642
#
# Clients then connect to: ws://127.0.0.1:9642/feed
set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
if [ -f "$project_dir/.env" ]; then
	set -a
	# shellcheck disable=SC1091
	. "$project_dir/.env"
	set +a
fi

rhf2_url=${FEED_URL_1:-tcp://0.0.0.0:19770}
# Prefer the first RHF2 slot if FEED_VENDOR_N is set.
i=1
while [ "$i" -le 64 ]; do
	eval "vendor=\${FEED_VENDOR_$i:-}"
	eval "url=\${FEED_URL_$i:-}"
	case "$(printf '%s' "$vendor" | tr '[:upper:]' '[:lower:]')" in
		rhf2)
			if [ -n "$url" ]; then
				rhf2_url=$url
			fi
			break
			;;
	esac
	i=$((i + 1))
done

nitro_host=${NITRO_FEED_HOST:-0.0.0.0}
nitro_port=${NITRO_FEED_PORT:-9642}
nitro_path=${NITRO_FEED_PATH:-/feed}

resolve_bin() {
	if [ -x "$project_dir/rhf2-relay" ]; then
		printf '%s\n' "$project_dir/rhf2-relay"
		return 0
	fi
	if [ -x "$project_dir/bin/rhf2-relay" ]; then
		printf '%s\n' "$project_dir/bin/rhf2-relay"
		return 0
	fi
	echo "Building rhf2-relay..." >&2
	mkdir -p "$project_dir/bin"
	(cd "$project_dir" && go build -trimpath -o "$project_dir/bin/rhf2-relay" ./cmd/rhf2-relay)
	printf '%s\n' "$project_dir/bin/rhf2-relay"
}

bin=$(resolve_bin)
exec "$bin" \
	--rhf2-url "$rhf2_url" \
	--nitro-host "$nitro_host" \
	--nitro-port "$nitro_port" \
	--nitro-path "$nitro_path" \
	"$@"
