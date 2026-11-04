#!/usr/bin/env bash
set -euo pipefail

head=${1:?head results json}
base=${2:-}
summary=${GITHUB_STEP_SUMMARY:-/dev/stdout}

{
  echo "## Course progress"
  echo
  jq -r 'group_by(.week)[] | "- **Week \(.[0].week)**: \(map(select(.status=="passed"))|length) of \(length) tests passing"' "$head"
  echo
  echo "Total: $(jq '[.[]|select(.status=="passed")]|length' "$head") of $(jq 'length' "$head") tests passing."
} >> "$summary"

if [ -z "$base" ] || [ ! -s "$base" ]; then
  echo "No baseline to compare against, so nothing can have regressed." >> "$summary"
  exit 0
fi

regressions=$(jq -r --slurpfile h "$head" '
  ($h[0] | map({key: .id, value: .status}) | from_entries) as $now
  | map(select(.status == "passed" and (($now[.id] // "missing") | IN("failed", "broken", "todo", "missing"))))
  | .[] | "\(.id)  (week \(.week) step \(.step), now \($now[.id] // "missing"))"' "$base")

if [ -n "$regressions" ]; then
  {
    echo
    echo "### Regressions"
    echo
    echo "These tests passed on the base commit and do not pass now:"
    echo
    echo "$regressions" | sed 's/^/- /'
  } >> "$summary"
  echo "$regressions" | sed 's/^/regressed: /'
  exit 1
fi
echo "No test that passed on the base commit fails now." >> "$summary"
