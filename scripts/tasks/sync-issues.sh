#!/usr/bin/env bash
# Sync docs/tasks/*.md into GitHub issues, labels and milestones.
# Idempotent: re-running updates labels and issue bodies, never duplicates.
#
# Usage:  scripts/tasks/sync-issues.sh            create or update everything
#         WEEK1=2026-09-07 scripts/tasks/sync-issues.sh   change the Monday of Week 1
#         ASSIGN=1 GH_VIGHNESH=SVIGHNESH GH_RAJAT=... GH_RITK=... GH_VIVEK=... scripts/tasks/sync-issues.sh
#                                                    also assign suggested owners (they must be collaborators)
set -euo pipefail

REPO="${REPO:-SVIGHNESH/RedForge}"
WEEK1="${WEEK1:-2026-09-07}"
ASSIGN="${ASSIGN:-0}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TASKS="$ROOT/docs/tasks"

log() { printf '%s\n' "$*" >&2; }
week_end() { date -d "$WEEK1 +$(( $1 * 7 - 1 )) days" +%F; }

# ---------- labels ----------
label() { gh label create "$1" -R "$REPO" --color "$2" --description "$3" --force >/dev/null; }
log "labels"
label "phase:0-foundation" 0E8A16 "Weeks 1 to 2, everyone together"
label "phase:1-tracks"     1D76DB "Weeks 3 to 8, parallel tracks"
label "phase:2-stretch"    FBCA04 "Weeks 9 to 11, stretch only after Must items"
label "phase:3-delivery"   5319E7 "Weeks 12 to 14, evaluation and delivery"
for m in 1 2 3 4 5 6 7 8 9 10 11; do label "item:M$m" 2DA44E "Must-Have item M$m (Proposal 4.1)"; done
for s in 1 2 3 4 5; do label "item:S$s" E99695 "Stretch item S$s (Proposal 4.2), cut-line rules apply"; done
label "owner:vighnesh" C5DEF5 "Suggested owner from the implementation plan"
label "owner:rajat"    C5DEF5 "Suggested owner from the implementation plan"
label "owner:ritk"     C5DEF5 "Suggested owner from the implementation plan"
label "owner:vivek"    C5DEF5 "Suggested owner from the implementation plan"
label "owner:anyone"   C5DEF5 "No suggested owner, anyone may take it"
label "blocked"        B60205 "A dependency is not merged yet"
label "task"           EDEDED "Generated from docs/tasks, do not edit the body by hand"

# ---------- milestones ----------
declare -A MS_NUM
milestone() {  # title week
  local title="$1" due; due="$(week_end "$2")T23:59:59Z"
  local num
  num=$(gh api "repos/$REPO/milestones?state=all&per_page=100" --jq ".[] | select(.title==\"$title\") | .number")
  if [ -z "$num" ]; then
    num=$(gh api "repos/$REPO/milestones" -f title="$title" -f due_on="$due" --jq .number)
    log "  created milestone $title"
  else
    gh api -X PATCH "repos/$REPO/milestones/$num" -f due_on="$due" >/dev/null
  fi
  MS_NUM["$title"]=$num
}
log "milestones (Week 1 starts $WEEK1)"
milestone "Phase 0: Foundation (Weeks 1 to 2)" 2
milestone "Week 6 checkpoint: M1 M3 M4 M5 M7 merged" 6
milestone "Week 8 checkpoint: all Must items except M10 M11" 8
milestone "Phase 2: Stretch (Weeks 9 to 11)" 11
milestone "Phase 3: Evaluation and delivery (Weeks 12 to 14)" 14

# ---------- existing issues ----------
declare -A ISSUE_OF   # task id -> issue number
while IFS=$'\t' read -r num title; do
  id=$(printf '%s' "$title" | grep -oE '^T[0-9]+\.[0-9]+' || true)
  [ -n "$id" ] && ISSUE_OF["$id"]=$num
done < <(gh issue list -R "$REPO" --state all --limit 500 --json number,title --jq '.[] | "\(.number)\t\(.title)"')

# ---------- parse one task file ----------
field() { grep -m1 -oE "\*\*$1:\*\*[^|]*" "$2" | sed -E "s/^\*\*$1:\*\* *//; s/ *$//" || true; }

owner_label() {
  local o; o=$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')
  case "$o" in
    *vighnesh*) echo owner:vighnesh ;;
    *rajat*)    echo owner:rajat ;;
    *ritk*)     echo owner:ritk ;;
    *vivek*)    echo owner:vivek ;;
    *)          echo owner:anyone ;;
  esac
}

milestone_for() {  # id week
  local id="$1" week="$2"
  case "$id" in
    T9.*) echo "Phase 2: Stretch (Weeks 9 to 11)"; return ;;
    T10.*|T11.02|T11.03|T11.04|T11.05) echo "Phase 3: Evaluation and delivery (Weeks 12 to 14)"; return ;;
  esac
  if   [ "$week" -le 2 ]; then echo "Phase 0: Foundation (Weeks 1 to 2)"
  elif [ "$week" -le 6 ]; then echo "Week 6 checkpoint: M1 M3 M4 M5 M7 merged"
  else echo "Week 8 checkpoint: all Must items except M10 M11"; fi
}

phase_label() {
  case "$1" in
    T0.*) echo phase:0-foundation ;;
    T9.*) echo phase:2-stretch ;;
    T10.*|T11.*) echo phase:3-delivery ;;
    *) echo phase:1-tracks ;;
  esac
}

build_body() {  # file id blocked_line
  local f="$1" id="$2" blocked="$3" base
  base=$(basename "$f")
  {
    tail -n +2 "$f"
    printf '\n---\n'
    [ -n "$blocked" ] && printf 'Blocked by: %s\n\n' "$blocked"
    printf 'Task file: https://github.com/%s/blob/main/docs/tasks/%s\n' "$REPO" "$base"
    printf 'Board: [docs/tasks/README.md](https://github.com/%s/blob/main/docs/tasks/README.md) | Plan: [docs/MASTER-PLAN.md](https://github.com/%s/blob/main/docs/MASTER-PLAN.md)\n\n' "$REPO" "$REPO"
    printf 'To claim: assign yourself and move the card to In Progress. Branch: `%s`. Open the PR with "Closes #<this issue>".\n' "${base%.md}"
  }
}

# ---------- pass 1: create or update issues ----------
log "issues"
for f in "$TASKS"/t*.md; do
  title=$(head -1 "$f" | sed 's/^# //')
  id=$(printf '%s' "$title" | grep -oE '^T[0-9]+\.[0-9]+' || true)
  item=$(field Item "$f");   week=$(field Week "$f" | grep -oE '[0-9]+' | head -1 || true)
  owner=$(field "Suggested owner" "$f")
  labels="task,$(phase_label "$id"),$(owner_label "$owner")"
  for it in $(printf '%s' "$item" | grep -oE '\b[MS][0-9]+\b' | sort -u || true); do labels="$labels,item:$it"; done
  ms=$(milestone_for "$id" "${week:-1}")
  body=$(build_body "$f" "$id" "")
  if [ -n "${ISSUE_OF[$id]:-}" ]; then
    n=${ISSUE_OF[$id]}
    gh issue edit "$n" -R "$REPO" --title "$title" --body "$body" --milestone "$ms" --add-label "$labels" >/dev/null
    log "  updated #$n $id"
  else
    url=$(gh issue create -R "$REPO" --title "$title" --body "$body" --milestone "$ms" --label "$labels")
    n=${url##*/}; ISSUE_OF["$id"]=$n
    log "  created #$n $id"
    sleep 1
  fi
  if [ "$ASSIGN" = "1" ]; then
    case "$(owner_label "$owner")" in
      owner:vighnesh) a="${GH_VIGHNESH:-}";; owner:rajat) a="${GH_RAJAT:-}";;
      owner:ritk) a="${GH_RITK:-}";;         owner:vivek) a="${GH_VIVEK:-}";; *) a="";;
    esac
    [ -n "$a" ] && gh issue edit "$n" -R "$REPO" --add-assignee "$a" >/dev/null
  fi
done

# ---------- pass 2: resolve dependencies into "Blocked by" links ----------
log "dependency links"
for f in "$TASKS"/t*.md; do
  id=$(head -1 "$f" | grep -oE 'T[0-9]+\.[0-9]+' | head -1 || true)
  deps=$(field "Depends on" "$f" | grep -oE 'T[0-9]+\.[0-9]+' | sort -u || true)
  blocked=""
  for d in $deps; do [ -n "${ISSUE_OF[$d]:-}" ] && blocked="$blocked #${ISSUE_OF[$d]}"; done
  blocked=${blocked# }
  body=$(build_body "$f" "$id" "$blocked")
  gh issue edit "${ISSUE_OF[$id]}" -R "$REPO" --body "$body" >/dev/null
done
log "done: ${#ISSUE_OF[@]} issues in https://github.com/$REPO/issues"
