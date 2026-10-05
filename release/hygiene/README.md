# Hygiene guards

Copies of three scripts from the repo-hygiene skill (revision bf73c1c), used by CI:

- `fork_scope.py` lists the files this fork owns.
- `added_comments.py` fails a pull request that adds whole-line comments to them.

A comment is allowed only as one line above the code it explains, starting with
`Invariant:`, `Race:`, `Quirk:` or `Protocol:`, and stating a reason the code cannot show.
The embedded WireGuard and upstream files are outside this rule.
