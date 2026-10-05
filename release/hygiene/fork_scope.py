#!/usr/bin/env python3
"""Who owns each file of a fork: upstream, the fork, or both. The pass cleans only what the fork owns.

usage: fork_scope.py --upstream REF [--head REF] [--derived DIR ...] [--tsv OUT.tsv] [--list CLASS[,CLASS]]
       fork_scope.py --upstream REF --old-upstream REF --old-head REF [--head REF]
       fork_scope.py --require-ancestor REF [REF ...] [--head REF]
       fork_scope.py --upstream REF --guard BOUNDARY_FILE              (CI check: nothing outside the boundary differs)

Mode 1, scope. Compares HEAD with the upstream revision the fork is based on and puts every tracked file in a class:
  UPSTREAM       byte-identical to upstream. Out of scope: no cleanup, no comments removed, no refactor.
  FORK_MODIFIED  an upstream file with fork changes. Only the fork's own lines are in scope; keep them small.
  FORK_ADDED     a file upstream does not have. Full scope.
  DERIVED        a FORK_ADDED file under a --derived directory: a vendored copy of another upstream with fork
                 changes. Bug fixes only; comments and layout stay so the next port compares line by line.
--list prints the paths of the named classes, one per line, for the --paths-from option of other scripts.

Mode 2, preservation. After an upstream update: was the fork's own change set kept? Compares the fork delta
before (old-upstream..old-head) with the delta after (upstream..head). Reports files that left the delta and
fork-added lines that are gone. Every reported line needs a reason: replaced on purpose, or adopted upstream.

Mode 3, ancestry. Is everything that already shipped inside this branch? Each REF (the default branch, the last
release tag, the upstream tag) must be an ancestor of HEAD. A branch cut from an old point silently drops the
fixes made after that point; a review that compares only with the merge base will not see it.

Mode 4, guard. BOUNDARY_FILE lists what the fork may differ in: sections [owned] and [derived] hold path patterns
(shell style, * also crosses /), section [hooks] holds `path +ADDED -REMOVED`, the largest size each fork hook
inside an upstream file may have. The working tree is compared with upstream; a changed file outside the
boundary, or a hook that grew, fails the check. Run it in CI and before each commit of a pass.

Exit status: 0 clean, 1 findings (mode 2: something left the delta; mode 3: a REF is not an ancestor;
mode 4: a file outside the boundary differs), 2 git error.
"""
import argparse
import collections
import fnmatch
import os
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from _common import repo_root


def git(root, *args):
    r = subprocess.run(['git', '-C', root, '-c', 'core.quotepath=false', *args], capture_output=True)
    if r.returncode not in (0, 1):
        sys.stderr.write(r.stderr.decode('utf-8', 'replace'))
        sys.exit(2)
    return r.returncode, r.stdout.decode('utf-8', 'replace')


def delta(root, a, b):
    """path -> (status, added, removed) for the diff a..b; binary files count 0 lines."""
    out = {}
    for ln in git(root, 'diff', '--name-status', '--no-renames', a, b)[1].splitlines():
        st, path = ln.split('\t', 1)
        out[path] = [st, 0, 0]
    for ln in git(root, 'diff', '--numstat', '--no-renames', a, b)[1].splitlines():
        add, rem, path = ln.split('\t', 2)
        if path in out:
            out[path][1] = int(add) if add.isdigit() else 0
            out[path][2] = int(rem) if rem.isdigit() else 0
    return out


def added_lines(root, a, b, path):
    c = collections.Counter()
    for ln in git(root, 'diff', '-U0', '--no-renames', a, b, '--', path)[1].splitlines():
        if ln.startswith('+') and not ln.startswith('+++') and ln[1:].strip():
            c[ln[1:].strip()] += 1
    return c


def scope(root, a):
    d = delta(root, a.upstream, a.head)
    derived = [x.rstrip('/') + '/' for x in a.derived]
    tracked = [p for p in git(root, 'ls-tree', '-r', '--name-only', a.head)[1].splitlines() if p]
    rows, counts, lines = [], collections.Counter(), collections.Counter()
    for path in tracked:
        st, add, rem = d.get(path, ('', 0, 0))
        if st == '':
            cls = 'UPSTREAM'
        elif st == 'A':
            cls = 'DERIVED' if any(path.startswith(x) for x in derived) else 'FORK_ADDED'
        else:
            cls = 'FORK_MODIFIED'
        counts[cls] += 1
        lines[cls] += add
        rows.append((path, cls, add, rem))
    deleted = sorted(p for p, v in d.items() if v[0] == 'D')
    if a.list:
        want = {x.strip().upper() for x in a.list.split(',')}
        for path, cls, _, _ in rows:
            if cls in want:
                print(path)
        return 0
    print(f'# fork scope: {a.head} against upstream {a.upstream}')
    print(f'# {len(tracked)} tracked files; fork changed {len(tracked) - counts["UPSTREAM"]} of them'
          + (f'; {len(deleted)} upstream files deleted by the fork' if deleted else ''))
    print('class\tfiles\tfork lines')
    for cls in ('UPSTREAM', 'FORK_MODIFIED', 'FORK_ADDED', 'DERIVED'):
        print(f'{cls}\t{counts[cls]}\t{lines[cls] if cls != "UPSTREAM" else "-"}')
    print('\n# FORK_MODIFIED files (upstream files carrying fork lines; keep these hooks small):')
    for path, cls, add, rem in rows:
        if cls == 'FORK_MODIFIED':
            print(f'{path}\t+{add}/-{rem}')
    for p in deleted:
        print(f'{p}\tDELETED by the fork')
    if a.tsv:
        with open(a.tsv, 'w', encoding='utf-8') as f:
            f.write('path\tclass\tadded\tremoved\n')
            for row in rows:
                if row[1] != 'UPSTREAM':
                    f.write('\t'.join(str(x) for x in row) + '\n')
    print('\n# Only FORK_ADDED is cleaned freely. UPSTREAM stays byte-identical. DERIVED and FORK_MODIFIED take bug fixes only.')
    return 0


def preservation(root, a):
    before = delta(root, a.old_upstream, a.old_head)
    after = delta(root, a.upstream, a.head)
    gone = sorted(set(before) - set(after))
    new = sorted(set(after) - set(before))
    print(f'# fork delta before: {len(before)} files ({a.old_upstream}..{a.old_head}); after: {len(after)} files ({a.upstream}..{a.head})')
    print(f'\n# left the delta: {len(gone)} (the fork change vanished, or upstream adopted it)')
    for p in gone:
        print(f'GONE\t{before[p][0]}\t{p}')
    print(f'\n# new in the delta: {len(new)}')
    for p in new[:200]:
        print(f'NEW\t{after[p][0]}\t{p}')
    total = 0
    print('\n# fork-added lines that are no longer fork-added lines (each needs a reason):')
    for p in sorted(set(before) & set(after)):
        lost = added_lines(root, a.old_upstream, a.old_head, p) - added_lines(root, a.upstream, a.head, p)
        n = sum(lost.values())
        if n:
            total += n
            print(f'LOST\t{n}\t{p}')
            for line, k in list(lost.items())[:a.samples]:
                print(f'\t{k}x | {line[:140]}')
    print(f'\n# {len(gone)} files left the delta, {total} fork lines changed or lost')
    return 1 if gone or total else 0


def ancestry(root, a):
    bad = 0
    for ref in a.require_ancestor:
        code, _ = git(root, 'merge-base', '--is-ancestor', ref, a.head)
        if code == 0:
            print(f'OK\t{ref} is an ancestor of {a.head}')
        else:
            bad += 1
            missing = git(root, 'rev-list', '--count', f'{a.head}..{ref}')[1].strip()
            base = git(root, 'merge-base', ref, a.head)[1].strip()[:12]
            print(f'MISSING\t{ref} is NOT an ancestor of {a.head}: {missing} commits are not in it (merge base {base})')
            for ln in git(root, 'log', '--oneline', '--no-merges', '-n', str(a.samples * 5), f'{a.head}..{ref}')[1].splitlines():
                print('\t' + ln)
    return 1 if bad else 0


def guard(root, a):
    patterns, hooks, section = [], {}, None
    for raw in open(a.guard, encoding='utf-8'):
        line = raw.strip()
        if not line or line.startswith('#'):
            continue
        if line.startswith('['):
            section = line.strip('[]')
        elif section == 'hooks':
            path, add, rem = line.split()
            hooks[path] = (int(add.lstrip('+')), int(rem.lstrip('-')))
        else:
            patterns.append(line)
    outside, grown, seen = [], [], set()
    for ln in git(root, 'diff', '--numstat', '--no-renames', a.upstream)[1].splitlines():
        add, rem, path = ln.split('\t', 2)
        seen.add(path)
        if any(fnmatch.fnmatchcase(path, pat) for pat in patterns):
            continue
        if path not in hooks:
            outside.append(path)
        elif (int(add) if add.isdigit() else 0) > hooks[path][0] or (int(rem) if rem.isdigit() else 0) > hooks[path][1]:
            grown.append(f'{path} +{add} -{rem} (allowed +{hooks[path][0]} -{hooks[path][1]})')
    for p in outside:
        print(f'OUTSIDE\t{p}\tupstream-owned file differs from upstream: move the change into a fork-owned file')
    for g in grown:
        print(f'GROWN\t{g}\tkeep a hook to a call into fork-owned code')
    for p in sorted(set(hooks) - seen):
        print(f'STALE\t{p}\tlisted as a hook but identical to upstream now: remove the line')
    print(f'# fork boundary: {len(seen)} files differ from {a.upstream}; {len(outside)} outside the boundary, {len(grown)} grown hooks')
    return 1 if outside or grown else 0


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument('--root', default='.')
    ap.add_argument('--upstream', help='upstream revision the fork is based on (a tag or commit)')
    ap.add_argument('--head', default='HEAD')
    ap.add_argument('--derived', action='append', default=[], help='directory holding a vendored, modified copy of another upstream')
    ap.add_argument('--tsv')
    ap.add_argument('--list', help='print only the paths of these classes, comma separated')
    ap.add_argument('--old-upstream')
    ap.add_argument('--old-head')
    ap.add_argument('--require-ancestor', nargs='+')
    ap.add_argument('--guard', help='boundary file; fail when anything outside it differs from --upstream')
    ap.add_argument('--samples', type=int, default=4)
    a = ap.parse_args()
    root = repo_root(a.root)
    if a.require_ancestor:
        sys.exit(ancestry(root, a))
    if not a.upstream:
        ap.error('--upstream is required')
    if a.guard:
        sys.exit(guard(root, a))
    if a.old_upstream or a.old_head:
        if not (a.old_upstream and a.old_head):
            ap.error('--old-upstream and --old-head go together')
        sys.exit(preservation(root, a))
    sys.exit(scope(root, a))


if __name__ == '__main__':
    main()
