#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

usage="usage: scripts/open-bump-pr.sh <version> <checks passed: true|false> <base branch> <run url>"
version="${1:?$usage}"
passed="${2:?$usage}"
base="${3:?$usage}"
run_url="${4:?$usage}"
branch=bump/whatsmeow
module=go.mau.fi/whatsmeow
bot_email="41898282+github-actions[bot]@users.noreply.github.com"
title="chore: bump whatsmeow to $version"

pr="$(gh pr list --head "$branch" --state open --json number,isDraft --jq '.[0] // empty | "\(.number) \(.isDraft)"')"
if [ -n "$pr" ]; then
  git fetch --quiet origin "$branch"
  branch_mod="$(mktemp)"
  trap 'rm -f "$branch_mod"' EXIT
  git show FETCH_HEAD:go.mod > "$branch_mod"
  if [ "$(go mod edit -json "$branch_mod" | jq -r --arg m "$module" '.Require[] | select(.Path == $m) | .Version')" = "$version" ]; then
    echo "PR #${pr%% *} already bumps to $version"
    exit 0
  fi
  if gh pr view "${pr%% *}" --json commits --jq '.commits[].authors[].email' | grep -qvxF "$bot_email"; then
    gh pr comment "${pr%% *}" --body "whatsmeow has a newer commit, $version. This PR has commits pushed by hand, so the bump leaves its branch alone: merge or close it, and the next run bumps again."
    exit 0
  fi
fi

git config user.name "github-actions[bot]"
git config user.email "$bot_email"
git switch -c "$branch"
git commit -am "$title"
git push --force origin "$branch"

if [ "$passed" = true ]; then
  result="The build and tests passed in [the bump run]($run_url)."
else
  result="**The build or tests failed** in [the bump run]($run_url). This draft needs a fix before it can merge."
fi
body="Bumps whatsmeow to commit [${version##*-}](https://github.com/tulir/whatsmeow/commits/${version##*-}).

$result

Before merging, try it against the real WhatsApp and cut a release as \"Following whatsmeow\" in the README describes."

if [ -z "$pr" ]; then
  create=(gh pr create --base "$base" --head "$branch" --title "$title" --body "$body")
  [ "$passed" = true ] || create+=(--draft)
  "${create[@]}"
else
  read -r number is_draft <<< "$pr"
  gh pr edit "$number" --title "$title" --body "$body"
  if [ "$passed" = true ] && [ "$is_draft" = true ]; then gh pr ready "$number"; fi
  if [ "$passed" != true ] && [ "$is_draft" = false ]; then gh pr ready --undo "$number"; fi
fi
