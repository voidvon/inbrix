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

for command in git gh node npm go; do
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

echo "==> Pushing release commit and tag"
git push --atomic origin "HEAD:main" "$tag"

echo "==> Waiting for the GitHub release workflow"
run_id=""
for _ in $(seq 1 30); do
  release_commit=$(git rev-parse HEAD)
  run_id=$(gh run list --workflow release.yml --event push --limit 20 --json databaseId,headSha --jq ".[] | select(.headSha == \"$release_commit\") | .databaseId" | head -n 1)
  [[ -n $run_id ]] && break
  sleep 2
done
[[ -n $run_id ]] || die "tag was pushed, but no release workflow run appeared; inspect GitHub Actions"
gh run watch "$run_id" --exit-status || die "GitHub release workflow failed for $tag"

release_url=$(gh release view "$tag" --json url --jq .url)
echo "Published $tag: $release_url"
