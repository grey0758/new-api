#!/usr/bin/env python3
"""Private, read-only-by-default NewAPI channel trace inspection. Never replays.

Run on the owning host. The default output contains metadata only; --payload is
an explicit local disclosure and must not be copied to shared reports.
"""
import argparse
import collections
import datetime as dt
import gzip
import json
import os
from pathlib import Path
import re
import time

NAME = re.compile(r'^trace-(\d{8}T\d{2})-[A-Za-z0-9]+-\d{6}\.jsonl\.gz$')
TTL = 86400
BUDGET = 2 * 1024**3
SCAN_LIMIT = 1024**3

def inspect(directory, action, request_id=None, trace_id=None, payload=False, limit=200):
    root = Path(directory)
    if not root.is_absolute() or root.is_symlink() or not root.is_dir():
        raise ValueError('invalid private trace directory')
    files = []
    for p in root.iterdir():
        match = NAME.fullmatch(p.name)
        if match and p.is_file() and not p.is_symlink():
            start = dt.datetime.strptime(match[1], '%Y%m%dT%H').replace(tzinfo=dt.timezone.utc).timestamp()
            files.append((start, p, p.stat().st_size))
    files.sort(key=lambda row: (row[0], row[1].name))
    now = time.time()
    if action == 'maintain':
        total = sum(row[2] for row in files)
        removed = 0
        for start, p, size in files:
            if start + TTL <= now or total > BUDGET:
                p.unlink(missing_ok=True)
                total -= size
                removed += 1
        for p in root.glob('stats-*.json'):
            if not p.is_symlink() and p.is_file() and p.stat().st_mtime < now - TTL:
                p.unlink()
        return {'retention_seconds': TTL, 'removed_segments': removed, 'compressed_bytes': total}
    if action == 'check':
        stats = []
        for p in root.glob('stats-*.json'):
            if p.is_file() and not p.is_symlink() and now-p.stat().st_mtime < 180:
                stats.append(json.loads(p.read_text()))
        return {'passed': bool(stats) and all(s.get('dropped_records') == 0 and s.get('io_errors') == 0 and s.get('retention_seconds') == TTL for s in stats),
                'active_stats': stats, 'compressed_bytes': sum(row[2] for row in files),
                'expired_segments': sum(start+TTL<=now for start, _, _ in files),
                'private_permissions': root.stat().st_mode & 0o777 == 0o700 and all(p.stat().st_mode & 0o777 == 0o600 for _, p, _ in files)}
    records = []
    errors = 0
    scanned = 0
    limited = False
    counts = collections.Counter()
    # Newest segments first; decompressed scan and result budgets are explicit.
    for _, p, _ in reversed(files):
        try:
            with gzip.open(p, 'rt') as source:
                for line in source:
                    scanned += len(line)
                    if scanned > SCAN_LIMIT:
                        limited = True
                        break
                    row = json.loads(line)
                    if request_id and row.get('request_id') != request_id:
                        continue
                    if trace_id and row.get('trace_id') != trace_id:
                        continue
                    counts[(row.get('channel_id'), row.get('stage'), row.get('error_code', ''))] += 1
                    if not payload:
                        row.pop('body', None)
                    records.append(row)
                    if len(records) >= limit:
                        limited = True
                        break
        except (OSError, EOFError, json.JSONDecodeError):
            # A final gzip member may still be flushing; never silently claim a
            # complete chain after a read failure or bounded scan.
            errors += 1
        if limited:
            break
    records.sort(key=lambda row: row.get('time', ''))
    return {'records': records, 'scan_bytes': scanned, 'scan_or_result_limited': limited, 'unreadable_segments': errors,
            'counts': [{'channel_id': k[0], 'stage': k[1], 'error_code': k[2], 'count': v} for k, v in counts.items()]}

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--directory', required=True)
    parser.add_argument('action', choices=['check', 'query', 'maintain'])
    selector = parser.add_mutually_exclusive_group()
    selector.add_argument('--request-id')
    selector.add_argument('--trace-id')
    parser.add_argument('--payload', action='store_true')
    parser.add_argument('--limit', type=int, default=200)
    args = parser.parse_args()
    if not 1 <= args.limit <= 1000:
        parser.error('limit must be 1..1000')
    result = inspect(args.directory, args.action, args.request_id, args.trace_id, args.payload, args.limit)
    print(json.dumps(result, ensure_ascii=False))
    if args.action == 'check' and (not result['passed'] or not result['private_permissions'] or result['expired_segments']):
        raise SystemExit(1)

if __name__ == '__main__':
    main()
