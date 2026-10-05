#!/usr/bin/env python3
"""Count whole-line comments ADDED by a change. Use it as the CI check that keeps a comment policy alive.

usage: added_comments.py [--root DIR] [--base REF] [--head REF] [--max N] [--allow REGEX] [--worktree] [--paths-from FILE]

Default base is the merge-base with origin/main (or origin/master, main, master); default head is HEAD.
Exit status: 0 when at most --max comment lines were added (default 0), 1 when more, 2 on a git or usage
error. Directive comments (shebang, shellcheck, noqa, auto-generated, region) never count, and neither does
a licence header at the top of a file. Pass --allow to exempt more patterns, for example an invariant tag
your project requires. Block comments are tracked inside each hunk; a line that closes a comment and then
continues with code is not counted.
"""
import argparse
import collections
import os
import re
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from _common import COMMENT_EXTS, LICENSE_RE, comment_flags, ext_of, is_directive, repo_root, run


def find_base(root):
    for ref in ('origin/main', 'origin/master', 'main', 'master'):
        if run('git', '-C', root, 'rev-parse', '--verify', '-q', ref).returncode == 0:
            mb = run('git', '-C', root, 'merge-base', ref, 'HEAD')
            if mb.returncode == 0 and mb.stdout.strip():
                return mb.stdout.strip()
    print('cannot find a base branch; pass --base', file=sys.stderr)
    sys.exit(2)


def unquote(p):
    if p.startswith('"') and p.endswith('"'):
        p = p[1:-1]
        p = re.sub(r'\\([0-7]{3})', lambda m: chr(int(m.group(1), 8)), p)
        p = p.encode('latin-1', 'replace').decode('utf-8', 'replace').replace('\\"', '"').replace('\\\\', '\\')
    return p


def parse_added(diff):
    """Yield (path, hunk) where hunk is a list of (new_line_number, text) for added lines."""
    path, hunk, new_line, in_hunk = None, [], 0, False
    out = []
    for ln in diff.split('\n'):
        if ln.startswith('diff --git '):
            if hunk and path:
                out.append((path, hunk))
            path, hunk, in_hunk = None, [], False
        elif not in_hunk and ln.startswith('+++ '):
            p = unquote(ln[4:])
            path = p[2:] if p.startswith('b/') else None
        elif ln.startswith('@@'):
            if hunk and path:
                out.append((path, hunk))
            hunk, in_hunk = [], True
            m = re.search(r'\+(\d+)', ln.split('@@')[1])
            new_line = int(m.group(1)) if m else 0
        elif in_hunk and ln.startswith('+'):
            hunk.append((new_line, ln[1:]))
            new_line += 1
    if hunk and path:
        out.append((path, hunk))
    return out


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument('--root', default='.')
    ap.add_argument('--base')
    ap.add_argument('--head', default='HEAD')
    ap.add_argument('--max', type=int, default=0)
    ap.add_argument('--allow', default='', help='regex of comment text that is allowed')
    ap.add_argument('--worktree', action='store_true', help='compare base with the working tree instead of --head')
    ap.add_argument('--samples', type=int, default=3)
    ap.add_argument('--paths-from', help='count only files listed in this file, one path per line (a fork: fork_scope.py --list FORK_ADDED)')
    a = ap.parse_args()
    root = repo_root(a.root)
    base = a.base or find_base(root)
    only = None
    if a.paths_from:
        only = {ln.strip() for ln in open(a.paths_from, encoding='utf-8') if ln.strip()}
    cmd = ['git', '-C', root, '-c', 'core.quotepath=false', 'diff', '-U0', '--no-color', '--diff-filter=AMRT', base]
    cmd += [] if a.worktree else [a.head]
    r = run(*cmd)
    if r.returncode != 0:
        print(r.stderr.strip(), file=sys.stderr)
        sys.exit(2)
    allow = re.compile(a.allow) if a.allow else None
    per_file = collections.defaultdict(list)
    for path, hunk in parse_added(r.stdout):
        if ext_of(path) not in COMMENT_EXTS or (only is not None and path not in only):
            continue
        flags = comment_flags(path, [t for _, t in hunk])
        for (n, text), is_c in zip(hunk, flags):
            if is_c and not is_directive(text) and not (allow and allow.search(text)):
                per_file[path].append((n, text.strip()))
    counts, samples = {}, {}
    for path, items in per_file.items():
        lic = [i for i, (n, t) in enumerate(items) if n <= 15 and LICENSE_RE.search(t)]
        drop = set()
        if lic:
            ns = {n: i for i, (n, _) in enumerate(items)}
            for i in lic:
                for step in (1, -1):
                    n = items[i][0]
                    while n in ns:
                        drop.add(ns[n])
                        n += step
        kept = [it for i, it in enumerate(items) if i not in drop]
        if kept:
            counts[path] = len(kept)
            samples[path] = [f'{n}: {t[:110]}' for n, t in kept[:a.samples]]
    total = sum(counts.values())
    print(f'base {base[:10]}  comment lines added: {total}  (max allowed {a.max})')
    for p, c in sorted(counts.items(), key=lambda kv: -kv[1]):
        print(f'  {c:4d}  {p}')
        for s in samples[p]:
            print(f'          {s}')
    sys.exit(1 if total > a.max else 0)


if __name__ == '__main__':
    main()
