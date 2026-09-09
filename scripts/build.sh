#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
npm --prefix web ci --no-audit --no-fund
npm --prefix web run build
node scripts/prepare-assets.mjs
mkdir -p bin
go build -trimpath -o bin/aegishook ./cmd/aegishook
