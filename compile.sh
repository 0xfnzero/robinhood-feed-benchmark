#!/bin/sh
set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
release_dir=${RELEASE_DIR:-"$project_dir/release"}
version=${VERSION:-dev}
targets=${TARGETS:-"x86_64-apple-darwin aarch64-apple-darwin x86_64-unknown-linux-gnu aarch64-unknown-linux-gnu"}
package_name=robinhood-feed-benchmark

mkdir -p "$release_dir"
staging_dir=$(mktemp -d "${TMPDIR:-/tmp}/robinhood-feed-release.XXXXXX")
cleanup() {
	rm -rf "$staging_dir"
}
trap cleanup EXIT HUP INT TERM

target_settings() {
	case "$1" in
		x86_64-apple-darwin) echo "darwin amd64" ;;
		aarch64-apple-darwin) echo "darwin arm64" ;;
		x86_64-unknown-linux-gnu) echo "linux amd64" ;;
		aarch64-unknown-linux-gnu) echo "linux arm64" ;;
		*)
			echo "Unsupported target: $1" >&2
			exit 1
			;;
	esac
}

for target in $targets; do
	settings=$(target_settings "$target")
	goos=${settings% *}
	goarch=${settings#* }
	archive="$release_dir/${package_name}-${target}.tar.gz"
	if [ -e "$archive" ]; then
		echo "Refusing to overwrite existing release asset: $archive" >&2
		echo "Use a new RELEASE_DIR or move the existing asset first." >&2
		exit 1
	fi

	package_dir="$staging_dir/$target/$package_name"
	mkdir -p "$package_dir"
	echo "==> Building $target"
	(
		cd "$project_dir"
		CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
			-trimpath -ldflags "-s -w -X main.version=$version" \
			-o "$package_dir/feed-benchmark" ./cmd/feed-benchmark
	)
	cp "$project_dir/run-feed-comparison.sh" "$package_dir/"
	if [ -f "$project_dir/run-vs-local-rbh.sh" ]; then
		cp "$project_dir/run-vs-local-rbh.sh" "$package_dir/"
		chmod 0755 "$package_dir/run-vs-local-rbh.sh"
	fi
	cp "$project_dir/.env.example" "$project_dir/.env.copy" "$package_dir/"
	cp "$project_dir/README.md" "$project_dir/README_CN.md" "$project_dir/LICENSE" "$package_dir/"
	printf '%s\n' "$version" > "$package_dir/VERSION"
	touch "$package_dir/.robinhood-feed-benchmark-install"
	chmod 0755 "$package_dir/feed-benchmark" "$package_dir/run-feed-comparison.sh"
	COPYFILE_DISABLE=1 tar -C "$staging_dir/$target" -czf "$archive" "$package_name"
	echo "    $archive"
done

cp "$project_dir/install.sh" "$release_dir/install.sh"
chmod 0755 "$release_dir/install.sh"
(
	cd "$release_dir"
	if command -v shasum >/dev/null 2>&1; then
		shasum -a 256 ${package_name}-*.tar.gz > SHA256SUMS
	elif command -v sha256sum >/dev/null 2>&1; then
		sha256sum ${package_name}-*.tar.gz > SHA256SUMS
	else
		echo "shasum or sha256sum is required" >&2
		exit 1
	fi
)

echo ""
echo "Release assets generated in $release_dir"
echo "Version: $version"
echo "Targets: $targets"
