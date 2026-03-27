#!/bin/sh
set -eu

cd "$(dirname "$0")/.."

if [ $# -eq 0 ]; then
  printf '%s\n' "Usage: release.sh <patch|minor|major>" >&2
  exit 1
fi

bump_type="$1"

current=$(node -p "require('./package.json').version")
IFS='.' read -r major minor patch <<EOF
$current
EOF

case "$bump_type" in
  patch) patch=$((patch + 1)) ;;
  minor) minor=$((minor + 1)); patch=0 ;;
  major) major=$((major + 1)); minor=0; patch=0 ;;
  *) printf '%s\n' "error: invalid bump type '$bump_type'. Use patch, minor, or major." >&2; exit 1 ;;
esac

new_version="$major.$minor.$patch"
tag="v$new_version"

printf '%s\n' "Bumping $current -> $new_version"

# Update package.json version
node -e "
  const fs = require('fs');
  const pkg = JSON.parse(fs.readFileSync('package.json', 'utf8'));
  pkg.version = '$new_version';
  fs.writeFileSync('package.json', JSON.stringify(pkg, null, 2) + '\n');
"

git add package.json
git commit -m "chore: release $tag"
git tag "$tag"
git push origin main "$tag"

printf '%s\n' "Released $tag. Now run: bun run build:release"
