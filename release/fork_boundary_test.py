"""Fork boundary check; run with Python stdlib unittest from the repository root.

The fork may change only what release/FORK_OWNED lists. Every other file must stay
byte-identical to the upstream commit named in release/FORK_UPSTREAM_BASE.
"""
from fnmatch import fnmatchcase
from pathlib import Path
import subprocess
import unittest

ROOT = Path(__file__).resolve().parents[1]


def git(*args):
    return subprocess.run(["git", "-C", str(ROOT), "-c", "core.quotepath=false", *args], capture_output=True, text=True)


def load_boundary():
    patterns, hooks, section = [], {}, None
    for raw in (ROOT / "release/FORK_OWNED").read_text().splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("["):
            section = line.strip("[]")
        elif section == "hooks":
            path, added, removed = line.split()
            hooks[path] = (int(added.lstrip("+")), int(removed.lstrip("-")))
        else:
            patterns.append(line)
    return patterns, hooks


class ForkBoundaryTests(unittest.TestCase):
    def test_only_owned_files_differ_from_upstream(self):
        base = (ROOT / "release/FORK_UPSTREAM_BASE").read_text().split()[0]
        ancestor = git("merge-base", "--is-ancestor", base, "HEAD")
        self.assertEqual(ancestor.returncode, 0, "upstream base %s is not an ancestor of HEAD (shallow clone? fetch the full history)" % base)
        diff = git("diff", "--numstat", "--no-renames", base)
        self.assertEqual(diff.returncode, 0, diff.stderr)
        patterns, hooks = load_boundary()
        outside, grown, seen = [], [], set()
        for row in diff.stdout.splitlines():
            added, removed, path = row.split("\t", 2)
            seen.add(path)
            if any(fnmatchcase(path, pattern) for pattern in patterns):
                continue
            if path not in hooks:
                outside.append(path)
                continue
            limit = hooks[path]
            if int(added) > limit[0] or int(removed) > limit[1]:
                grown.append("%s +%s -%s (allowed +%d -%d)" % (path, added, removed, limit[0], limit[1]))
        self.assertEqual(outside, [], "upstream-owned files were changed; move the change into a fork-owned file")
        self.assertEqual(grown, [], "a hook in an upstream file grew; keep hooks to a call into fork-owned code")
        self.assertEqual(sorted(set(hooks) - seen), [], "hooks listed in release/FORK_OWNED that no longer differ from upstream")


if __name__ == "__main__":
    unittest.main()
