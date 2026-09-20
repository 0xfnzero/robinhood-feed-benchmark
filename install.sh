#!/bin/sh
set -eu

repo=${REPO:-0xfnzero/robinhood-feed-benchmark}
version=${VERSION:-latest}
install_dir=${INSTALL_DIR:-"$HOME/robinhood-feed-benchmark"}
target=${TARGET:-}
package_name=robinhood-feed-benchmark
install_marker=.robinhood-feed-benchmark-install
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

detect_target() {
	case "$(uname -s)" in
		Darwin) os=apple-darwin ;;
		Linux) os=unknown-linux-gnu ;;
		*)
			echo "Unsupported OS: $(uname -s). Supported: macOS and Linux." >&2
			exit 1
			;;
	esac
	case "$(uname -m)" in
		x86_64|amd64) arch=x86_64 ;;
		arm64|aarch64) arch=aarch64 ;;
		*)
			echo "Unsupported architecture: $(uname -m). Supported: x86_64 and arm64." >&2
			exit 1
			;;
	esac
	echo "${arch}-${os}"
}

download() {
	url=$1
	output=$2
	if command -v curl >/dev/null 2>&1; then
		curl -fL --retry 3 -o "$output" "$url"
	elif command -v wget >/dev/null 2>&1; then
		wget -q --show-progress -O "$output" "$url"
	else
		echo "curl or wget is required" >&2
		exit 1
	fi
}

checksum() {
	file=$1
	if command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$file" | awk '{print $1}'
	elif command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$file" | awk '{print $1}'
	else
		echo "shasum or sha256sum is required" >&2
		exit 1
	fi
}

verify_checksum() {
	sums_file=$1
	archive_file=$2
	expected=$(awk -v name="$archive" '$2 == name || $2 == "*" name {print $1; exit}' "$sums_file")
	if [ -z "$expected" ]; then
		echo "Checksum entry not found for $archive" >&2
		exit 1
	fi
	actual=$(checksum "$archive_file")
	if [ "$actual" != "$expected" ]; then
		echo "Checksum mismatch for $archive" >&2
		exit 1
	fi
}

case "$install_dir" in
	""|/|/.|.|..|"$HOME"|"$HOME/")
		echo "Refusing unsafe INSTALL_DIR: $install_dir" >&2
		exit 1
		;;
esac

if [ -z "$target" ]; then
	target=$(detect_target)
fi
archive="${package_name}-${target}.tar.gz"
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/robinhood-feed-install.XXXXXX")
cleanup() {
	rm -rf "$work_dir"
}
trap cleanup EXIT HUP INT TERM

if [ -n "${PACKAGE_FILE:-}" ]; then
	archive_file=$PACKAGE_FILE
	if [ ! -f "$archive_file" ]; then
		echo "PACKAGE_FILE does not exist: $archive_file" >&2
		exit 1
	fi
	if [ -n "${CHECKSUM_FILE:-}" ]; then
		verify_checksum "$CHECKSUM_FILE" "$archive_file"
	fi
elif [ -f "$script_dir/$archive" ]; then
	archive_file="$script_dir/$archive"
	if [ ! -f "$script_dir/SHA256SUMS" ]; then
		echo "Missing SHA256SUMS next to local release package" >&2
		exit 1
	fi
	verify_checksum "$script_dir/SHA256SUMS" "$archive_file"
else
	if [ -n "${BASE_URL:-}" ]; then
		base_url=${BASE_URL%/}
	elif [ "$version" = latest ]; then
		base_url="https://github.com/$repo/releases/latest/download"
	else
		base_url="https://github.com/$repo/releases/download/$version"
	fi
	archive_file="$work_dir/$archive"
	download "$base_url/$archive" "$archive_file"
	download "$base_url/SHA256SUMS" "$work_dir/SHA256SUMS"
	verify_checksum "$work_dir/SHA256SUMS" "$archive_file"
fi

tar -xzf "$archive_file" -C "$work_dir"
package_dir="$work_dir/$package_name"
if [ ! -f "$package_dir/$install_marker" ] || [ ! -x "$package_dir/feed-benchmark" ]; then
	echo "Invalid release package: required files are missing" >&2
	exit 1
fi

if [ -e "$install_dir" ]; then
	if [ ! -d "$install_dir" ] || [ ! -f "$install_dir/$install_marker" ]; then
		echo "Refusing to replace a directory not managed by this installer: $install_dir" >&2
		echo "Choose a different INSTALL_DIR." >&2
		exit 1
	fi
	backup="${install_dir}.backup.$(date +%Y%m%d%H%M%S)"
	if [ -e "$backup" ]; then
		echo "Backup path already exists: $backup" >&2
		exit 1
	fi
	mv "$install_dir" "$backup"
	if ! mv "$package_dir" "$install_dir"; then
		mv "$backup" "$install_dir"
		echo "Installation failed; previous installation restored" >&2
		exit 1
	fi
	if [ -f "$backup/.env" ]; then
		cp -p "$backup/.env" "$install_dir/.env"
	fi
	echo "Previous installation backed up to: $backup"
else
	mkdir -p "$(dirname "$install_dir")"
	mv "$package_dir" "$install_dir"
fi

chmod 0755 "$install_dir/feed-benchmark" "$install_dir/run-feed-comparison.sh"

echo ""
echo "Robinhood Feed Benchmark installed"
echo "Target: $target"
echo "Install directory: $install_dir"
echo ""
echo "Configure Feeds in .env, then run:"
echo "  cd \"$install_dir\""
echo "  cp .env.copy .env"
echo "  edit .env"
echo "  ./run-feed-comparison.sh"
echo ""
echo "Set FEED_INCLUDE_OFFICIAL=true in .env if you want the official Feed."
