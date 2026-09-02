#!/usr/bin/env bash
# Test runner for Git Bash / Linux / macOS.  Usage: ./test.sh [mode]
set -uo pipefail

cd "$(dirname "$0")/app" || exit 1
MODE="${1:-all}"

case "$MODE" in
  unit)
    echo "==> unit tests"
    go test -run TestHandle -v ./...
    ;;
  e2e)
    echo "==> end-to-end tests"
    go test -run TestE2E -v ./...
    ;;
  all)
    echo "==> vet"
    go vet ./... || exit 1
    echo "==> all tests"
    go test ./...
    ;;
  cover)
    echo "==> coverage"
    go test -coverprofile=coverage.out ./... || exit 1
    go tool cover -func=coverage.out
    go tool cover -html=coverage.out -o coverage.html
    echo "==> wrote app/coverage.html"
    ;;
  strict)
    # Shuffled and repeated: catches order dependence and flakes. These
    # tests change the working directory and HOME, so this is the run that
    # actually proves they are independent.
    echo "==> vet"
    go vet ./... || exit 1
    echo "==> strict: shuffled, 3 repeats"
    go test -shuffle=on -count=3 -timeout 5m ./...
    ;;
  fuzz)
    # Generates new inputs hunting for a crash. Ctrl-C to stop early; any
    # crash found is saved to testdata/fuzz/ and replays as a normal test.
    DURATION="${2:-30s}"
    echo "==> fuzzing each target for $DURATION"
    for target in FuzzHandleInput FuzzHandleEcho FuzzHandleTYPE; do
      echo "--- $target"
      go test -fuzz "$target" -fuzztime "$DURATION" -run '^$' ./... || exit 1
    done
    ;;
  *)
    echo "usage: ./test.sh [unit|e2e|all|cover|strict|fuzz [duration]]"
    exit 2
    ;;
esac
