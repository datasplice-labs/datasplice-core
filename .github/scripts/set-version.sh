#!/usr/bin/env bash

set -euo pipefail

# The workflow passes the event's base and head commits. A push uses the
# previous pushed commit as its base; a pull request uses the target branch.
version_file="${VERSION_FILE:-version/VERSION}"
base_sha="${BASE_SHA:?BASE_SHA is required}"
head_sha="${HEAD_SHA:?HEAD_SHA is required}"

current_version=$(tr -d '\r\n' < "$version_file")
# In Bash regexes, ^ anchors the match at the start and $ anchors it at the
# end. Capturing groups let BASH_REMATCH split both semvers into components.
version_pattern='^dev-([0-9]+)\.([0-9]+)\.([0-9]+) \(([0-9]+)\.([0-9]+)\.([0-9]+)\)$'

# =~ applies an extended regular expression. The optional scoped part allows
# both "feat:" and "feat(parser):"; ! marks a breaking feature.
feat_breaking_pattern='^feat(\([^)]*\))?!:'
feat_pattern='^feat(\([^)]*\))?:'
fix_pattern='^fix(\([^)]*\))?:'

if [[ ! "$current_version" =~ $version_pattern ]]; then
  printf 'invalid version in %s: %s\n' "$version_file" "$current_version" >&2
  exit 1
fi

build_major=${BASH_REMATCH[1]}
build_minor=${BASH_REMATCH[2]}
build_patch=${BASH_REMATCH[3]}
commit_major=${BASH_REMATCH[4]}
commit_minor=${BASH_REMATCH[5]}
commit_patch=${BASH_REMATCH[6]}

# GitHub uses an all-zero SHA when a push has no previous commit, such as the
# first push to a branch. ^0+$ means "only zeros, from start to finish".
if [[ "$base_sha" =~ ^0+$ ]]; then
  commit_range=("$head_sha")
else
  commit_range=("$base_sha..$head_sha")
fi

mapfile -t subjects < <(git log --format=%s "${commit_range[@]}")

# git log is newest-first, but a merged push usually starts with a synthetic
# "Merge pull request ..." commit. Find the newest commit that affects the
# build version instead of assuming the first subject is relevant.
last_subject=""
for subject in "${subjects[@]}"; do
  if [[ "$subject" =~ $feat_breaking_pattern ]] ||
    [[ "$subject" =~ $feat_pattern ]] ||
    [[ "$subject" =~ $fix_pattern ]]; then
    last_subject="$subject"
    break
  fi
done

if [[ "$last_subject" =~ $feat_breaking_pattern ]]; then
  ((build_major += 1))
  build_minor=0
  build_patch=0
elif [[ "$last_subject" =~ $feat_pattern ]]; then
  ((build_minor += 1))
  build_patch=0
elif [[ "$last_subject" =~ $fix_pattern ]]; then
  ((build_patch += 1))
fi

for subject in "${subjects[@]}"; do
  if [[ "$subject" =~ $feat_breaking_pattern ]]; then
    ((commit_major += 1))
  elif [[ "$subject" =~ $feat_pattern ]]; then
    ((commit_minor += 1))
  elif [[ "$subject" =~ $fix_pattern ]]; then
    ((commit_patch += 1))
  fi
done

printf 'dev-%s.%s.%s (%s.%s.%s)\n' \
  "$build_major" "$build_minor" "$build_patch" \
  "$commit_major" "$commit_minor" "$commit_patch" > "$version_file"
printf 'version: %s\n' "$(tr -d '\r\n' < "$version_file")"