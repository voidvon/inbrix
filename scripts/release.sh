#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"

die() {
  echo "发布失败：$*" >&2
  exit 1
}

next_version() {
  local version=$1 major minor patch
  [[ $version =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]] || die "VERSION '$version' 格式无效，应为 X.Y.Z"
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
    [[ $actual == "$expected" ]] || die "版本规则测试失败：$test_case -> $actual，预期为 $expected"
  done <<'CASES'
0.1.0 0.1.1
0.1.19 0.1.20
0.1.20 0.2.0
0.19.20 0.20.0
0.20.0 1.0.0
1.17.0 1.17.1
1.20.0 2.0.0
CASES
  echo "发布版本规则：通过"
}

if [[ ${1:-} == "--selftest" ]]; then
  selftest
  exit 0
fi
if [[ ${1:-} == "--next" ]]; then
  [[ $# == 2 ]] || die "用法：scripts/release.sh --next X.Y.Z"
  next_version "$2"
  exit 0
fi
[[ $# == 0 ]] || die "用法：make release"

for command in git gh node npm go make zip shasum; do
  command -v "$command" >/dev/null || die "缺少必需命令：$command"
done
gh auth status >/dev/null 2>&1 || die "GitHub CLI 尚未登录，请运行 'gh auth login'"

branch=$(git branch --show-current)
[[ $branch == "main" ]] || die "只能从 main 分支发布（当前分支：${branch:-游离 HEAD}）"
[[ -z $(git status --porcelain) ]] || die "工作区存在未提交的更改，请先提交或暂存后再发布"

git fetch origin main --tags
local_head=$(git rev-parse HEAD)
remote_head=$(git rev-parse origin/main)
[[ $local_head == "$remote_head" ]] || die "本地 main 与 origin/main 不同步"

current=$(tr -d '[:space:]' < VERSION)
latest_tag=$(git describe --tags --abbrev=0 2>/dev/null || true)
[[ $latest_tag == "v$current" ]] || die "VERSION 为 $current，但当前提交可达的最新标签为 ${latest_tag:-无}"
next=$(next_version "$current")
tag="v$next"
git rev-parse --verify --quiet "refs/tags/$tag" >/dev/null && die "标签 $tag 已存在"

echo "==> 准备 $tag"
node scripts/update-release-version.mjs "$current" "$next"
go run ./site/gen
git diff --check
git add VERSION README.md README_CN.md site/index.html site/docs.html site/docs
git diff --cached --quiet && die "更新版本后没有产生任何更改"
git commit -m "chore: 准备发布 $tag"
git tag -a "$tag" -m "$tag"

echo "==> 运行 $tag 发布检查"
if ! make check; then
  die "检查失败；$tag 及其发布提交仍保留在本地，尚未推送"
fi
[[ -z $(git status --porcelain) ]] || die "发布检查修改了已跟踪文件，请检查并提交后再发布"

echo "==> 在本地构建发布文件"
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

echo "==> 推送发布提交和标签"
git push --atomic origin "HEAD:main" "$tag"

notes_file="$asset_dir/release-notes.md"
repo=$(gh repo view --json nameWithOwner --jq .nameWithOwner)
{
  printf '%s\n\n' '## 使用前请先校验文件'
  printf '%s\n' '```sh'
  printf 'curl -fsSLO https://raw.githubusercontent.com/%s/%s/scripts/verify.sh\n' "$repo" "$tag"
  printf 'bash verify.sh --repo %s --tag %s inbrix_%s_linux_amd64.zip\n' "$repo" "$tag" "$version"
  printf '%s\n\n' '```'
  printf '%s\n' '发布文件中包含 SHA256SUMS，请在运行前校验下载的压缩包。'
} > "$notes_file"
echo "==> 创建 GitHub Release 并上传发布文件"
gh release create "$tag" "${assets[@]}" "$asset_dir/SHA256SUMS" --repo "$repo" --verify-tag --draft --title "$tag" --notes-file "$notes_file"
gh release edit "$tag" --repo "$repo" --draft=false

release_url=$(gh release view "$tag" --repo "$repo" --json url --jq .url)
echo "已发布 $tag：$release_url"
