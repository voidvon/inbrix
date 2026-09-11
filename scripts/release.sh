#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"

die() {
  echo "release: $*" >&2
  exit 1
}

next_version() {
  local version=$1 major minor patch
  [[ $version =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]] || die "invalid VERSION '$version'; expected X.Y.Z"
  major=$((10#${BASH_REMATCH[1]}))
  minor=$((10#${BASH_REMATCH[2]}))
  patch=$((10#${BASH_REMATCH[3]}))

  if (( minor >= 20 )); then
    printf '%d.0.0\n' "$((major + 1))"
  elif (( patch >= 20 )); then
    printf '%d.%d.0\n' "$major" "$((minor + 1))"
  else
    printf '%d.%d.%d\n' "$major" "$minor" "$((patch + 1))"
  fi
}

selftest() {
  local test_case actual
  while IFS=' ' read -r test_case expected; do
    actual=$(next_version "$test_case")
    [[ $actual == "$expected" ]] || die "version rule failed: $test_case -> $actual, expected $expected"
  done <<'CASES'
0.1.0 0.1.1
0.1.19 0.1.20
0.1.20 0.2.0
0.19.20 0.20.0
0.20.0 1.0.0
1.17.0 1.17.1
1.20.0 2.0.0
CASES
  echo "release version rules: ok"
}

if [[ ${1:-} == "--selftest" ]]; then
  selftest
  exit 0
fi
if [[ ${1:-} == "--next" ]]; then
  [[ $# == 2 ]] || die "usage: scripts/release.sh --next X.Y.Z"
  next_version "$2"
  exit 0
fi
[[ $# == 0 ]] || die "usage: make release"

for command in git gh node npm go make zip shasum; do
  command -v "$command" >/dev/null || die "required command not found: $command"
done
gh auth status >/dev/null 2>&1 || die "GitHub CLI is not authenticated; run 'gh auth login'"

branch=$(git branch --show-current)
[[ $branch == "main" ]] || die "releases must be created from main (current branch: ${branch:-detached HEAD})"
[[ -z $(git status --porcelain) ]] || die "working tree is not clean; commit or stash changes before releasing"

git fetch origin main --tags
local_head=$(git rev-parse HEAD)
remote_head=$(git rev-parse origin/main)
[[ $local_head == "$remote_head" ]] || die "local main is not synchronized with origin/main"

current=$(tr -d '[:space:]' < VERSION)
latest_tag=$(git describe --tags --abbrev=0 2>/dev/null || true)
[[ $latest_tag == "v$current" ]] || die "VERSION is $current but latest reachable tag is ${latest_tag:-none}"
next=$(next_version "$current")
tag="v$next"
git rev-parse --verify --quiet "refs/tags/$tag" >/dev/null && die "tag $tag already exists"

echo "==> Preparing $tag"
node scripts/update-release-version.mjs "$current" "$next"
go run ./site/gen
git diff --check
git add VERSION README.md README_CN.md site/index.html site/docs.html site/docs
git diff --cached --quiet && die "version update produced no changes"
git commit -m "chore: prepare $tag release"
git tag -a "$tag" -m "$tag"

echo "==> Running release checks for $tag"
if ! make check; then
  die "checks failed; $tag and its release commit remain local and were not pushed"
fi
[[ -z $(git status --porcelain) ]] || die "release checks changed tracked files; review and commit them before publishing"

echo "==> Building release assets locally"
asset_dir=$(mktemp -d)
trap 'rm -rf "$asset_dir"' EXIT
version="$next"
ldflags="-s -w -X main.Version=$version"
mkdir -p "$asset_dir/inbrix"
cp config.toml.example "$asset_dir/inbrix/config.toml.example"

for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  IFS=/ read -r goos goarch <<< "$target"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -ldflags "$ldflags" -o "$asset_dir/inbrix/inbrix" .
  (cd "$asset_dir" && zip -qr "inbrix_${version}_${goos}_${goarch}.zip" inbrix)
  rm "$asset_dir/inbrix/inbrix"
done
git archive --format=zip HEAD -o "$asset_dir/inbrix_${version}_source.zip"
(cd "$asset_dir" && shasum -a 256 inbrix_*.zip > SHA256SUMS)
assets=("$asset_dir"/inbrix_*.zip)
bash scripts/verify.sh --dir "$asset_dir" "${assets[@]##*/}"

echo "==> Pushing release commit and tag"
git push --atomic origin "HEAD:main" "$tag"

notes_file="$asset_dir/release-notes.md"
repo=$(gh repo view --json nameWithOwner --jq .nameWithOwner)
{
  printf '%s\n\n' '## Verify before you run this'
  printf '%s\n' '```sh'
  printf 'curl -fsSLO https://raw.githubusercontent.com/%s/%s/scripts/verify.sh\n' "$repo" "$tag"
  printf 'bash verify.sh --repo %s --tag %s inbrix_%s_linux_amd64.zip\n' "$repo" "$tag" "$version"
  printf '%s\n\n' '```'
  printf '%s\n' 'The release assets include SHA256SUMS. Verify an archive before running it.'
} > "$notes_file"
echo "==> Creating GitHub Release and uploading assets"
gh release create "$tag" "${assets[@]}" "$asset_dir/SHA256SUMS" --repo "$repo" --verify-tag --draft --title "$tag" --notes-file "$notes_file"
gh release edit "$tag" --repo "$repo" --draft=false

release_url=$(gh release view "$tag" --repo "$repo" --json url --jq .url)
echo "Published $tag: $release_url"
