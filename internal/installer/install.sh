#!/usr/bin/env sh
# Installs infr (InferenceRig) from GitHub releases.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/antonikliment/InferenceRig/main/internal/installer/install.sh | sh
#   curl -fsSL .../install.sh | sh -s -- v0.1.0-alpha.2   # pin a release
#
# Env vars:
#   INSTALL_DIR   where to place the binary (default: /usr/local/bin, falls
#                 back to ~/.local/bin)
set -eu

repo="antonikliment/InferenceRig"
version=""

for arg in "$@"; do
	case "$arg" in
	-h | --help)
		sed -n '2,10p' "$0" 2>/dev/null || echo "see script header for usage" >&2
		exit 0
		;;
	v*) version=$arg ;;
	*)
		echo "unknown argument: $arg" >&2
		exit 1
		;;
	esac
done

os=$(uname -s)
arch=$(uname -m)

case "$os" in
Linux) os=linux ;;
Darwin) os=darwin ;;
*)
	echo "unsupported OS: $os" >&2
	exit 1
	;;
esac

case "$arch" in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*)
	echo "unsupported architecture: $arch" >&2
	exit 1
	;;
esac

if [ -z "$version" ]; then
	# Use the releases list, not /releases/latest: the latter excludes
	# prereleases and 404s while only prereleases exist. The list is newest
	# first, so the first tag_name is the most recent release.
	version=$(curl -fsSL "https://api.github.com/repos/$repo/releases?per_page=1" | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
	if [ -z "$version" ]; then
		echo "could not determine latest release version" >&2
		exit 1
	fi
fi

name="inferencerig_${version}_${os}_${arch}"
base_url="https://github.com/$repo/releases/download/$version"

workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT

echo "downloading $name.tar.gz ($version)..." >&2
curl -fsSL -o "$workdir/$name.tar.gz" "$base_url/$name.tar.gz"
curl -fsSL -o "$workdir/SHA256SUMS" "$base_url/SHA256SUMS"

# macOS ships shasum, not sha256sum. shasum has no --ignore-missing, so the
# single relevant line is selected before checking.
if command -v sha256sum >/dev/null 2>&1; then
	(cd "$workdir" && sha256sum --ignore-missing --check SHA256SUMS)
elif command -v shasum >/dev/null 2>&1; then
	(cd "$workdir" && grep " $name\.tar\.gz\$" SHA256SUMS | shasum -a 256 -c -)
else
	echo "error: neither sha256sum nor shasum found; cannot verify download" >&2
	exit 1
fi

tar -C "$workdir" -xzf "$workdir/$name.tar.gz"

install_dir=${INSTALL_DIR:-/usr/local/bin}
if [ ! -w "$install_dir" ] 2>/dev/null; then
	install_dir="${INSTALL_DIR:-$HOME/.local/bin}"
	mkdir -p "$install_dir"
fi

install -m 755 "$workdir/$name/infr" "$install_dir/infr"
echo "installed $("$install_dir/infr" version) to $install_dir/infr" >&2

case ":$PATH:" in
*":$install_dir:"*) ;;
*) echo "note: $install_dir is not on your PATH" >&2 ;;
esac
