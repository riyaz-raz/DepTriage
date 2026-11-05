#!/bin/sh
# Install the latest deptriage release. Usage: curl -fsSL https://raw.githubusercontent.com/deptriage/deptriage/main/install.sh | sh
set -eu

repo="${DEPTRIAGE_REPO:-deptriage/deptriage}"
prefix="${DEPTRIAGE_INSTALL_DIR:-${HOME}/.local/bin}"
os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"

case "$os" in
  linux|darwin) ;;
  *) echo "deptriage: unsupported operating system: $os" >&2; exit 1 ;;
esac
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "deptriage: unsupported architecture: $arch" >&2; exit 1 ;;
esac

command -v curl >/dev/null 2>&1 || { echo "deptriage: curl is required" >&2; exit 1; }
command -v tar >/dev/null 2>&1 || { echo "deptriage: tar is required" >&2; exit 1; }

version="$(curl -fsSL "https://api.github.com/repos/${repo}/releases/latest" | awk -F'"' '/"tag_name"/ { print $4; exit }')"
[ -n "$version" ] || { echo "deptriage: could not determine the latest release" >&2; exit 1; }

archive="deptriage_${version#v}_${os}_${arch}.tar.gz"
base_url="https://github.com/${repo}/releases/download/${version}"
tmpdir="$(mktemp -d 2>/dev/null || mktemp -d -t deptriage)"
trap 'rm -rf "$tmpdir"' EXIT

curl -fsSL -o "$tmpdir/$archive" "$base_url/$archive"
curl -fsSL -o "$tmpdir/checksums.txt" "$base_url/checksums.txt"

expected="$(awk -v file="$archive" '$2 == file { print $1; exit }' "$tmpdir/checksums.txt")"
[ -n "$expected" ] || { echo "deptriage: checksum entry not found" >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmpdir/$archive" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  actual="$(shasum -a 256 "$tmpdir/$archive" | awk '{print $1}')"
else
  echo "deptriage: sha256sum or shasum is required to verify the download" >&2
  exit 1
fi
[ "$expected" = "$actual" ] || { echo "deptriage: checksum verification failed" >&2; exit 1; }

mkdir -p "$prefix"
tar -xzf "$tmpdir/$archive" -C "$tmpdir"
install -m 0755 "$tmpdir/deptriage" "$prefix/deptriage"
echo "Installed deptriage ${version} to ${prefix}/deptriage"
case ":${PATH}:" in
  *":${prefix}:"*) ;;
  *) echo "Add ${prefix} to PATH if it is not already there." ;;
esac
