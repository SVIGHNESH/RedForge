#!/usr/bin/env bash
# Create the GitHub Projects (v2) kanban board and add every task issue to it.
# Needs the "project" OAuth scope once:  gh auth refresh -h github.com -s project
#
# Usage: scripts/tasks/create-board.sh
set -euo pipefail

REPO="${REPO:-SVIGHNESH/RedForge}"
OWNER="${OWNER:-${REPO%%/*}}"
TITLE="${TITLE:-RedForge Task Board}"

log() { printf '%s\n' "$*" >&2; }

if ! gh auth status 2>&1 | grep -q "'project'\|, project\|project,"; then
  log "The gh token lacks the 'project' scope. Run once, then re-run this script:"
  log "    gh auth refresh -h github.com -s project"
  exit 1
fi

# find or create the project
num=$(gh project list --owner "$OWNER" --format json --limit 100 \
      --jq ".projects[] | select(.title==\"$TITLE\") | .number" | head -1)
if [ -z "$num" ]; then
  num=$(gh project create --owner "$OWNER" --title "$TITLE" --format json --jq .number)
  log "created project #$num"
fi
pid=$(gh project view "$num" --owner "$OWNER" --format json --jq .id)
gh project link "$num" --owner "$OWNER" --repo "$REPO" >/dev/null 2>&1 || true

# Status field and its "Todo" option
fields=$(gh project field-list "$num" --owner "$OWNER" --format json)
status_id=$(printf '%s' "$fields" | jq -r '.fields[] | select(.name=="Status") | .id')
todo_id=$(printf '%s' "$fields" | jq -r '.fields[] | select(.name=="Status") | .options[] | select(.name=="Todo") | .id')

# issues already on the board
declare -A ON_BOARD
while IFS=$'\t' read -r item url; do ON_BOARD["$url"]=$item; done < <(
  gh project item-list "$num" --owner "$OWNER" --format json --limit 500 \
    --jq '.items[] | select(.content.url != null) | "\(.id)\t\(.content.url)"')

# add every task issue
while IFS=$'\t' read -r url state; do
  if [ -z "${ON_BOARD[$url]:-}" ]; then
    item=$(gh project item-add "$num" --owner "$OWNER" --url "$url" --format json --jq .id)
    ON_BOARD["$url"]=$item
    log "added $url"
    if [ "$state" = "OPEN" ] && [ -n "$todo_id" ]; then
      gh project item-edit --project-id "$pid" --id "$item" --field-id "$status_id" --single-select-option-id "$todo_id" >/dev/null
    fi
  fi
done < <(gh issue list -R "$REPO" --label task --state all --limit 500 --json url,state --jq '.[] | "\(.url)\t\(.state)"')

log "board: https://github.com/users/$OWNER/projects/$num"
log "Next, in the board UI: Settings > Status > add an 'In Review' option between In Progress and Done,"
log "and Workflows > enable 'Item added to project' and 'Pull request merged' so cards move automatically."
