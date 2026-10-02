#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
source_file="$(mktemp)"
mirror_file="$(mktemp)"
trap 'unlink "$source_file" 2>/dev/null || true; unlink "$mirror_file" 2>/dev/null || true' EXIT

curl -fsSL --max-time 60 https://chatgpt.com/codex/install.sh -o "$source_file"
python3 - "$source_file" "$mirror_file" <<'PY'
from pathlib import Path
import sys

source = Path(sys.argv[1]).read_text()
replacements = {
    'RELEASES_BASE_URL="https://releases.openai.com/codex"':
        'RELEASES_BASE_URL="https://api.opencodex.uk/codex-cache"',
    '      warn "releases.openai.com is unavailable; falling back to GitHub Releases."':
        '      echo "Codex R2 mirror is unavailable; retry later." >&2\n      exit 1',
    '  resolve_release_from_github "$normalized_version"\n  select_release_assets\n}':
        '  echo "Codex R2 mirror is required for this installer." >&2\n  exit 1\n}',
    'download_fallback_url="$(release_url_for_asset "$asset" "$resolved_version")"':
        'download_fallback_url=""',
    'checksum_fallback_url="$(release_url_for_asset "$checksum_asset" "$resolved_version")"':
        'checksum_fallback_url=""',
}
if not source.startswith('#!/bin/sh'):
    raise SystemExit('Unexpected official installer header')
for old, new in replacements.items():
    if source.count(old) != 1:
        raise SystemExit(f'Official installer changed near {old[:40]!r}')
    source = source.replace(old, new)
Path(sys.argv[2]).write_text(source)
PY
sh -n "$mirror_file"
test -n "${CLOUDFLARE_API_TOKEN:-}" || { echo 'CLOUDFLARE_API_TOKEN must be supplied by the operator' >&2; exit 1; }
cd "$script_dir"
npx --yes wrangler r2 object put opencodex-codex-release-cache/install.sh \
  --file "$mirror_file" --remote --content-type 'text/x-shellscript; charset=utf-8'
