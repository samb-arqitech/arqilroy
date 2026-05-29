#!/bin/sh
# Poll GitHub PR checks until required CI jobs pass or fail.
# Evidence: .ai/runs/$KILROY_RUN_ID/test-evidence/latest/ci/
set -eu

KILROY_RUN_ID="${KILROY_RUN_ID:-}"
GITHUB_REPO="${GITHUB_REPO:-}"
PR_NUMBER="${PR_NUMBER:-}"
CI_REQUIRED_JOBS="${CI_REQUIRED_JOBS:-Quality Checks,Backend Tests,E2E Tests (UI),Documentation Validation,Contract Quality Checks}"
CI_POLL_TIMEOUT_SEC="${CI_POLL_TIMEOUT_SEC:-3600}"
CI_POLL_INTERVAL_SEC="${CI_POLL_INTERVAL_SEC:-60}"

if [ -z "$KILROY_RUN_ID" ]; then
  echo "KILROY_VALIDATE_FAILURE: KILROY_RUN_ID is required" >&2
  exit 1
fi

if [ -z "$GITHUB_REPO" ]; then
  echo "KILROY_VALIDATE_FAILURE: GITHUB_REPO is required (e.g. Arqitech-Inc/atomic-swap-app)" >&2
  exit 1
fi

if ! command -v gh >/dev/null 2>&1; then
  echo "KILROY_VALIDATE_FAILURE: gh CLI not found; install and authenticate (gh auth login or GH_TOKEN)" >&2
  exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "KILROY_VALIDATE_FAILURE: jq is required to parse gh pr checks JSON" >&2
  exit 1
fi

EVIDENCE_DIR=".ai/runs/${KILROY_RUN_ID}/test-evidence/latest/ci"
mkdir -p "$EVIDENCE_DIR"

if [ -z "$PR_NUMBER" ]; then
  branch="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || true)"
  if [ -z "$branch" ] || [ "$branch" = "HEAD" ]; then
    echo "KILROY_VALIDATE_FAILURE: cannot resolve PR_NUMBER (set PR_NUMBER or run on a named branch)" >&2
    exit 1
  fi
  PR_NUMBER="$(gh pr view --repo "$GITHUB_REPO" --head "$branch" --json number -q '.number' 2>/dev/null || true)"
  if [ -z "$PR_NUMBER" ]; then
    echo "KILROY_VALIDATE_FAILURE: no open PR for branch $branch on $GITHUB_REPO" >&2
    exit 1
  fi
fi

pr_url="https://github.com/${GITHUB_REPO}/pull/${PR_NUMBER}"
echo "verify-pr-ci: polling PR #${PR_NUMBER} (${pr_url})"

run_branch="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")"
if [ -n "$run_branch" ] && [ "$run_branch" != "HEAD" ]; then
  if git rev-parse "@{u}" >/dev/null 2>&1; then
    ahead="$(git rev-list --count "@{u}..HEAD" 2>/dev/null || echo 0)"
  else
    ahead=1
  fi
  if [ "$ahead" -gt 0 ]; then
    git push origin "HEAD:${run_branch}" 2>/dev/null || git push -u origin "$run_branch" 2>/dev/null || true
  fi
fi

start_ts=$(date +%s)
deadline=$((start_ts + CI_POLL_TIMEOUT_SEC))
checks_summary="${EVIDENCE_DIR}/checks-summary.json"
verify_md="${EVIDENCE_DIR}/verify-ci.md"

fetch_checks_json() {
  gh pr checks "$PR_NUMBER" --repo "$GITHUB_REPO" --json name,state,bucket,link,startedAt,completedAt 2>/dev/null || echo '[]'
}

normalize_state() {
  raw="$(echo "$1" | tr '[:lower:]' '[:upper:]')"
  case "$raw" in
    SUCCESS|PASS) echo pass ;;
    SKIPPED|NEUTRAL) echo skip ;;
    FAILURE|FAIL|CANCELLED|TIMED_OUT|ACTION_REQUIRED|ERROR) echo fail ;;
    *) echo pending ;;
  esac
}

check_state() {
  req="$1"
  checks_json="$2"
  raw="$(echo "$checks_json" | jq -r --arg n "$req" '.[] | select(.name == $n) | .state' | head -1)"
  if [ -z "$raw" ]; then
    echo pending
    return
  fi
  normalize_state "$raw"
}

write_summary() {
  checks_json="$(fetch_checks_json)"
  echo "$checks_json" > "$checks_summary"
  echo "$checks_json" | jq -r '.[] | "\(.name)\t\(.state)"' > "${EVIDENCE_DIR}/checks-raw.txt" 2>/dev/null || true
}

poll_count=0
while [ "$(date +%s)" -lt "$deadline" ]; do
  poll_count=$((poll_count + 1))
  checks_json="$(fetch_checks_json)"
  write_summary

  all_required_done=1
  any_failed=0
  pending_list=""
  fail_list=""
  pass_list=""

  old_ifs=$IFS
  IFS=,
  for job in $CI_REQUIRED_JOBS; do
    IFS=$old_ifs
    job=$(echo "$job" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    [ -n "$job" ] || continue
    IFS=,
    state="$(check_state "$job" "$checks_json")"
    case "$state" in
      pass|skip)
        pass_list="${pass_list}${pass_list:+, }${job}(${state})"
        ;;
      fail)
        any_failed=1
        fail_list="${fail_list}${fail_list:+, }${job}(${state})"
        ;;
      *)
        all_required_done=0
        pending_list="${pending_list}${pending_list:+, }${job}(${state})"
        ;;
    esac
  done
  IFS=$old_ifs

  {
    echo "# CI Verification — PR #${PR_NUMBER}"
    echo ""
    echo "- Repo: \`${GITHUB_REPO}\`"
    echo "- PR: ${pr_url}"
    echo "- Poll: ${poll_count} ($(date -u +%Y-%m-%dT%H:%M:%SZ))"
    echo "- Required jobs: ${CI_REQUIRED_JOBS}"
    echo ""
    echo "## Status"
    if [ "$any_failed" -eq 1 ]; then
      echo "**FAIL** — failing: ${fail_list}"
    elif [ "$all_required_done" -eq 1 ]; then
      echo "**PASS** — all required jobs green: ${pass_list}"
    else
      echo "**PENDING** — waiting: ${pending_list}"
    fi
  } > "$verify_md"

  if [ "$any_failed" -eq 1 ]; then
    echo "KILROY_VALIDATE_FAILURE: CI checks failed: ${fail_list}" >&2
    exit 1
  fi

  if [ "$all_required_done" -eq 1 ]; then
    echo "verify-pr-ci: all required jobs passed"
    exit 0
  fi

  echo "verify-pr-ci: pending (${pending_list}); sleep ${CI_POLL_INTERVAL_SEC}s"
  sleep "$CI_POLL_INTERVAL_SEC"
done

echo "KILROY_VALIDATE_FAILURE: CI poll timed out after ${CI_POLL_TIMEOUT_SEC}s" >&2
exit 1
