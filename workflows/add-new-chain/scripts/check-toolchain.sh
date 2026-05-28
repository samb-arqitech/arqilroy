#!/bin/sh
set -e
trap 'echo "KILROY_VALIDATE_FAILURE: check-toolchain.sh crashed at line $LINENO"' EXIT

# Minimal toolchain gate for atomic-swap-app chain integration workflow.
if ! command -v bun >/dev/null 2>&1; then
  echo "KILROY_VALIDATE_FAILURE: bun is required"
  exit 1
fi
if ! command -v git >/dev/null 2>&1; then
  echo "KILROY_VALIDATE_FAILURE: git is required"
  exit 1
fi
if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "KILROY_VALIDATE_FAILURE: workspace must be a git repository"
  exit 1
fi

trap - EXIT
echo "toolchain ok"
