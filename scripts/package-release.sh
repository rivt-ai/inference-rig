#!/usr/bin/env bash
set -euo pipefail

version=${1:?usage: package-release.sh VERSION COMMIT COMMIT_TIME [OUTPUT_DIR]}
commit=${2:?usage: package-release.sh VERSION COMMIT COMMIT_TIME [OUTPUT_DIR]}
commit_time=${3:?usage: package-release.sh VERSION COMMIT COMMIT_TIME [OUTPUT_DIR]}
output_dir=${4:-dist/release}

if [[ ! $version =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-([0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$ ]]; then
	echo "release version must be prerelease SemVer prefixed with v" >&2
	exit 1
fi
IFS=. read -r -a prerelease_parts <<<"${BASH_REMATCH[4]}"
for part in "${prerelease_parts[@]}"; do
	if [[ $part =~ ^[0-9]+$ && ${#part} -gt 1 && $part == 0* ]]; then
		echo "numeric prerelease identifiers must not contain leading zeroes" >&2
		exit 1
	fi
done
if [[ ! $commit =~ ^[0-9a-f]{40}$ ]]; then
	echo "commit must be a full lowercase Git SHA" >&2
	exit 1
fi

mkdir -p "$output_dir"
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
archives=()
ldflags="-s -w -X inferencerig/internal/buildinfo.Version=$version -X inferencerig/internal/buildinfo.Commit=$commit -X inferencerig/internal/buildinfo.CommitTime=$commit_time"

for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
	os=${target%/*}
	arch=${target#*/}
	name="inferencerig_${version}_${os}_${arch}"
	package_dir="$stage/$name"
	mkdir -p "$package_dir"
	CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "$ldflags" -o "$package_dir/infr" .
	cp LICENSE README.md "$package_dir/"
	go version -m "$package_dir/infr" >/dev/null
	archive="$output_dir/$name.tar.gz"
	tar -C "$stage" -czf "$archive" "$name"
	archives+=("$archive")
done

cp scripts/install.sh "$output_dir/install.sh"

(cd "$output_dir" && sha256sum "${archives[@]##*/}" install.sh >SHA256SUMS)
