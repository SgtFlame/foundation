#!/usr/bin/env python3
"""Splits a shared markdown file into fragment files, once, for docs-index.sh.

    scripts/docs-split.py sections <file.md> <fragment dir>
    scripts/docs-split.py blocks   <file.md> <fragment dir>

`sections` starts a fragment at every heading; `blocks` at every paragraph,
table row and top-level list item. Fragments are numbered in steps of 10
(`0010-<slug>.md`), so later work can put one between two others. Then add
"<fragment dir> <file.md>" to scripts/docs-index.conf and run docs-index.sh.
"""
import os, re, sys

def slug(line):
    s = re.sub(r"\]\([^)]*\)", "", line)
    s = re.sub(r"[^A-Za-z0-9]+", "-", s).strip("-").lower()
    return (s[:48].rstrip("-")) or "x"

def sections(lines):
    out, cur, fence = [], [], False
    for l in lines:
        if l.lstrip().startswith("```"):
            fence = not fence
        if not fence and l.startswith("#") and cur:
            out.append(cur); cur = []
        cur.append(l)
    if cur: out.append(cur)
    return out

def blocks(lines):
    paras, cur, fence = [], [], False
    for l in lines:
        if l.lstrip().startswith("```"):
            fence = not fence
        if not fence and not l.strip():
            if cur: paras.append(cur); cur = []
            continue
        cur.append(l)
    if cur: paras.append(cur)
    out = []
    for p in paras:
        if p[0].lstrip().startswith("```"):
            out.append(p); continue
        cur = []
        i = 0
        while i < len(p):
            l = p[i]
            if l.startswith("|"):
                if cur: out.append(cur); cur = []
                if i + 1 < len(p) and re.match(r"^\|[\s:|-]+\|\s*$", p[i + 1]):
                    out.append([l, p[i + 1]]); i += 2; continue
                out.append([l]); i += 1; continue
            if re.match(r"^([-*]|\d+\.) ", l):
                if cur: out.append(cur)
                cur = [l]; i += 1; continue
            if l.startswith("#"):
                if cur: out.append(cur); cur = []
                out.append([l]); i += 1; continue
            cur.append(l); i += 1
        if cur: out.append(cur)
    return out

def main():
    mode, src, dst = sys.argv[1:4]
    lines = open(src, encoding="utf-8").read().split("\n")
    while lines and not lines[-1].strip(): lines.pop()
    frags = sections(lines) if mode == "sections" else blocks(lines)
    os.makedirs(dst, exist_ok=True)
    for n, f in enumerate(frags, 1):
        while f and not f[-1].strip(): f.pop()
        while f and not f[0].strip(): f.pop(0)
        if not f: continue
        name = "%04d-%s.md" % (n * 10, slug(f[0]))
        with open(os.path.join(dst, name), "w", encoding="utf-8") as w:
            w.write("\n".join(f) + "\n")
    print("%s: %d fragments in %s" % (src, len(frags), dst))

main()
