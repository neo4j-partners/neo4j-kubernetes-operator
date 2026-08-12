#!/usr/bin/env python3
"""Check helm-fields _index.csv for FR mapping gaps.

Fails (exit 1) when rows have client_need but no fr_ids and are not deferred/out_of_scope.

Usage:
  python design/analysis/helm-fields/scripts/check-fr-coverage.py
  python design/analysis/helm-fields/scripts/check-fr-coverage.py --report
"""
from __future__ import annotations

import argparse
import csv
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
INDEX = ROOT / "_index.csv"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--report", action="store_true", help="Print gap lines only")
    args = parser.parse_args()

    if not INDEX.exists():
        print(f"Missing {INDEX}", file=sys.stderr)
        return 1

    gaps: list[tuple[str, str]] = []
    total_with_need = 0
    mapped = 0

    with INDEX.open(encoding="utf-8") as f:
        for row in csv.DictReader(f):
            need = (row.get("client_need") or "").strip()
            fr_ids = (row.get("fr_ids") or "").strip()
            status = (row.get("status") or "").strip()

            if not need or need.startswith("UNKNOWN"):
                continue

            total_with_need += 1
            if fr_ids:
                mapped += 1
                continue

            if status in ("deferred", "reviewed"):
                # reviewed without fr_ids is still a gap unless out_of_scope noted in need
                if "out_of_scope" in need.lower() or "n/a" in need.lower():
                    mapped += 1
                    continue

            gaps.append((row.get("helm_path", "?"), need[:80]))

    if args.report:
        for path, need in gaps:
            print(f"{path}\t{need}")
        return 0

    pct = (mapped / total_with_need * 100) if total_with_need else 100.0
    print(f"FR mapping: {mapped}/{total_with_need} ({pct:.1f}%)")

    if gaps:
        print(f"\n{len(gaps)} unmapped helm paths with client_need:", file=sys.stderr)
        for path, need in gaps[:20]:
            print(f"  - {path}: {need}", file=sys.stderr)
        if len(gaps) > 20:
            print(f"  ... and {len(gaps) - 20} more", file=sys.stderr)
        return 1

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
