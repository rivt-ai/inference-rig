#!/usr/bin/env sh
# Installs infr (InferenceRig) from GitHub releases.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/rivt-ai/inference-rig/main/internal/installer/install.sh | sh
#   curl -fsSL .../install.sh | sh -s -- dev              # newest release, prereleases included
#   curl -fsSL .../install.sh | sh -s -- v0.1.0-alpha.2   # pin an exact release
#
# Channels:
#   stable (default)  latest non-prerelease release
#   dev               latest release, prereleases included
#
# Env vars:
#   INSTALL_DIR   where to place the binary (default: /usr/local/bin, falls
#                 back to ~/.local/bin)
#   COMMAND_NAME  installed command name (default: infr)
set -eu

repo="rivt-ai/inference-rig"

# The repository was renamed from rivt-ai/InferenceRig. GitHub redirects the
# API and download URLs, so $repo alone is enough to fetch — but a build
# provenance attestation records the repository as it was named at build time,
# and `gh attestation verify` checks that name rather than following the
# rename. Releases published before the rename therefore verify only against
# the old name, and later ones only against the new. Both are accepted, in
# newest-first order; verification fails only when no name verifies.
attestation_repos="rivt-ai/inference-rig rivt-ai/InferenceRig"
channel="stable"
version=""

for arg in "$@"; do
	case "$arg" in
	-h | --help)
		sed -n '2,16p' "$0" 2>/dev/null || echo "see script header for usage" >&2
		exit 0
		;;
	stable | dev) channel=$arg ;;
	v*) version=$arg ;;
	*)
		echo "unknown argument: $arg" >&2
		exit 1
		;;
	esac
done

command_name=${COMMAND_NAME:-infr}
case "$command_name" in
*[!A-Za-z0-9._-]*)
	echo "error: invalid COMMAND_NAME '$command_name'; use only letters, digits, '.', '_', and '-'" >&2
	exit 1
	;;
esac

install_dir=${INSTALL_DIR:-/usr/local/bin}
if [ ! -w "$install_dir" ] 2>/dev/null; then
	install_dir="${INSTALL_DIR:-$HOME/.local/bin}"
	mkdir -p "$install_dir"
fi

if existing_command=$(command -v "$command_name" 2>/dev/null); then
	if [ ! -e "$install_dir/$command_name" ] || [ ! "$existing_command" -ef "$install_dir/$command_name" ]; then
		echo "error: command '$command_name' already exists at $existing_command; set COMMAND_NAME to install InferenceRig under another name" >&2
		exit 1
	fi
fi

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
	if [ "$channel" = stable ]; then
		# /releases/latest is GitHub's own "latest non-prerelease" resolution;
		# it 404s while only prereleases exist, which is a clear failure rather
		# than silently falling back to a prerelease.
		version=$(curl -fsSL "https://api.github.com/repos/$repo/releases/latest" | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
		if [ -z "$version" ]; then
			echo "could not resolve the stable channel: no non-prerelease release exists yet (try 'dev')" >&2
			exit 1
		fi
	else
		# The releases list is newest first regardless of prerelease status, so
		# the first tag_name is the newest release of any kind.
		version=$(curl -fsSL "https://api.github.com/repos/$repo/releases?per_page=1" | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
		if [ -z "$version" ]; then
			echo "could not determine latest release version" >&2
			exit 1
		fi
	fi
fi

name="inferencerig_${version}_${os}_${arch}"
base_url="https://github.com/$repo/releases/download/$version"

workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT

echo "installing $version ($os/$arch) to $install_dir/$command_name..." >&2
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

# Build provenance verification is best-effort: gh is not a hard dependency of
# this script, and releases published before provenance attestation existed
# have none to check (HTTP 404, not a mismatch) — only an attestation that
# exists and fails to verify indicates real tampering.
if command -v gh >/dev/null 2>&1; then
	attestation_verified=""
	attestation_missing=""
	attestation_output=""
	for attestation_repo in $attestation_repos; do
		if attestation_output=$(gh attestation verify "$workdir/$name.tar.gz" --repo "$attestation_repo" 2>&1); then
			attestation_verified=$attestation_repo
			break
		fi
		# Only a 404 means "this release has no attestation at all". A name
		# the release was not built under fails differently, and that is not
		# evidence of tampering while another name remains to try.
		if echo "$attestation_output" | grep -q "HTTP 404"; then
			attestation_missing=yes
		fi
	done
	if [ -n "$attestation_verified" ]; then
		echo "$attestation_output" >&2
	elif [ -n "$attestation_missing" ]; then
		echo "note: no build provenance attestation found for $name.tar.gz (release predates attestation); skipping" >&2
	else
		echo "$attestation_output" >&2
		echo "error: build provenance verification failed for $name.tar.gz" >&2
		exit 1
	fi
else
	echo "note: gh not found; skipping build provenance verification (install gh and re-run 'gh attestation verify' to check it yourself)" >&2
fi

tar -C "$workdir" -xzf "$workdir/$name.tar.gz"

install -m 755 "$workdir/$name/infr" "$install_dir/$command_name"
echo "installed $("$install_dir/$command_name" version) to $install_dir/$command_name" >&2

case ":$PATH:" in
*":$install_dir:"*) ;;
*) echo "note: $install_dir is not on your PATH" >&2 ;;
esac
