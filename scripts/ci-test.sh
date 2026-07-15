#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../server"
go test ./... -count=1
go vet ./...
