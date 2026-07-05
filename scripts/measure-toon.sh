#!/usr/bin/env bash
# Measure TOON vs JSON payload size on real Asana data (EXPERIMENTAL).
#
# Byte size is a proxy for token count — TOON's real win is dropping repeated
# object keys, which tends to show up somewhat larger in tokens than in bytes,
# so treat these numbers as a conservative floor. Run after `go build -o dharma
# ./cmd/dharma`, with a working Asana token configured.
#
#   ./scripts/measure-toon.sh [task-gid] [project-gid]
set -euo pipefail

DHARMA=${DHARMA:-./dharma}

measure() {
  local label="$1"; shift
  local jbytes tbytes pct
  jbytes=$("$DHARMA" "$@" --output json 2>/dev/null | wc -c | tr -d ' ')
  tbytes=$("$DHARMA" "$@" --output toon 2>/dev/null | wc -c | tr -d ' ')
  if [ "$jbytes" -eq 0 ]; then
    printf "%-28s (no output — skipped)\n" "$label"
    return
  fi
  pct=$(( (jbytes - tbytes) * 100 / jbytes ))
  printf "%-28s json=%7s  toon=%7s  saved=%3s%%\n" "$label" "$jbytes" "$tbytes" "$pct"
}

echo "TOON vs JSON (compact) — bytes, proxy for tokens"
echo "------------------------------------------------"
measure "workspace list (flat)"  workspace list
measure "project list (flat)"    project list --paginate
measure "my-tasks (flat)"        my-tasks list --limit 100
measure "user me (nested obj)"   user me
if [ "${1:-}" ]; then
  measure "task get (nested obj)"  task get "$1" --no-context
fi
if [ "${2:-}" ]; then
  # Nested array: assignee.name yields a nested object per row, so TOON falls
  # back and shows little benefit — included to make that visible.
  measure "task list +assignee (nested)" task list --project "$2" --fields name,completed,due_on,assignee.name
fi
