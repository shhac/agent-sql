---
description: Build, release, and publish to Homebrew
argument-hint: <patch|minor|major>
---

# Release

Perform a full release: version bump, build, GitHub release, and Homebrew tap update.

## Arguments

- `$ARGUMENTS` — version bump type: `patch`, `minor`, or `major`

## Instructions

You are performing a release of the `agent-sql` CLI. Follow these steps exactly.

### Pre-flight

1. Confirm the working tree is clean (`git st`). If not, stop and ask.
2. Run `bun test` and `bun run typecheck`. If either fails, stop and fix.
3. Show the user what version bump will happen (current version from `package.json` + bump type).

### Step 1: Version bump, tag, and push

```bash
bun run release <bump-type>
```

The release script bumps `package.json`, commits, tags, and pushes.

Capture the new version number from the output for subsequent steps. Verify the tag was pushed successfully before continuing.

### Step 2: Build release binaries

Clean up any leftover artifacts from previous builds, then build:

```bash
rm -f release/agent-sql-* release/checksums-sha256.txt
bun run build:release
```

This creates binaries in `release/` for all platforms (darwin-arm64, darwin-x64, linux-x64, linux-x64-musl, linux-arm64, linux-arm64-musl, windows-x64) plus `checksums-sha256.txt`. Verify all 7 binaries and the checksum file exist in `release/` before continuing.

### Step 3: Create tarballs

```bash
cd release
# Tarball each non-Windows binary
for bin in agent-sql-darwin-arm64 agent-sql-darwin-x64 agent-sql-linux-x64 agent-sql-linux-x64-musl agent-sql-linux-arm64 agent-sql-linux-arm64-musl; do
  tar czf "${bin}.tar.gz" "$bin"
done
# Regenerate checksums to include tarballs
shasum -a 256 *.tar.gz agent-sql-windows-x64.exe > checksums-sha256.txt
```

### Step 4: Create GitHub release

Generate release notes from commits since the previous tag:

```bash
# Get the previous tag (empty if first release)
prev_tag=$(git tag --sort=-v:refname | head -2 | tail -1)
# Get commit subjects between tags
git log --pretty=format:"- %s" "${prev_tag}..v<NEW_VERSION>" --no-merges | grep -v "^- v[0-9]"
```

For the first release (no previous tag), use all commits:

```bash
git log --pretty=format:"- %s" "v<NEW_VERSION>" --no-merges | grep -v "^- v[0-9]"
```

Use those to write concise release notes, then create the release:

```bash
gh release create v<NEW_VERSION> release/*.tar.gz release/agent-sql-windows-x64.exe release/checksums-sha256.txt \
  --title "v<NEW_VERSION>" \
  --notes "<release notes>"
```

This upload can be slow — use a 300s timeout for large Bun-compiled binaries.

Verify the release was created: `gh release view v<NEW_VERSION>`. If the upload timed out, retry with `gh release upload v<NEW_VERSION> <missing-files>`.

### Step 5: Update Homebrew tap

The Homebrew formula lives in the `homebrew-tap` sibling repo (`../homebrew-tap` relative to this repo's root).

First, check if the sibling repo exists:

```bash
ls ../homebrew-tap/Formula/agent-sql.rb
```

**If it doesn't exist:** Skip this step and tell the user: "Homebrew tap not found at `../homebrew-tap/Formula/agent-sql.rb`. Update the formula manually or create one in the homebrew-tap repo."

**If it exists:** Read the SHA256 checksums from `release/checksums-sha256.txt` and update the formula:

1. Read `../homebrew-tap/Formula/agent-sql.rb`
2. Update:
   - `version` to the new version (bare number, no `v` prefix, e.g., `"0.1.0"`)
   - All `url` lines to use `v<NEW_VERSION>`
   - All `sha256` values from the checksums (match darwin-arm64, darwin-x64, linux-arm64, linux-x64 tarballs)
   - The `assert_match` version string in the test block
3. Write the updated formula
4. Commit and push:
   ```bash
   cd ../homebrew-tap
   git add Formula/agent-sql.rb
   git commit -m "agent-sql <NEW_VERSION>"
   git push
   ```

### Step 6: Report

Show the user:

- New version number
- GitHub release URL
- Homebrew tap commit (if applicable)
- `brew upgrade shhac/tap/agent-sql` command for users
