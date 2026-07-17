#!/usr/bin/env bash
set -euo pipefail

FILE="${1:-backend/migrations/0001_init.up.sql}"

echo "Database migration file hint: ${FILE}"
echo "Use your migration tool (golang-migrate or atlas). This repo only stores SQL, execution command is project-specific."
