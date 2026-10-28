"""Release safety regression checks; run with Python stdlib unittest."""
from pathlib import Path
import re
import unittest

WORKFLOW = Path(__file__).resolve().parents[1] / ".github/workflows/release.yml"


class ReleaseSecurityTests(unittest.TestCase):
    def test_action_pins(self):
        for action in re.findall(r"uses:\s+([^\s]+)", WORKFLOW.read_text()):
            self.assertRegex(action, r"^[^@]+@[0-9a-f]{40}$")

    def test_fork_workflows_pin_patched_go(self):
        for name in ("ci.yml", "verify.yml", "release.yml"):
            versions = re.findall(r"go-version:\s*['\"]?([\d.x]+)", (WORKFLOW.parent / name).read_text())
            self.assertTrue(versions, name)
            for version in versions:
                self.assertRegex(version, r"^\d+\.\d+\.\d+$", name)
                self.assertGreaterEqual(tuple(map(int, version.split("."))), (1, 26, 10), name)

    def test_no_clobber_or_forced_tag(self):
        text = WORKFLOW.read_text()
        self.assertNotIn("--clobber", text)
        self.assertNotRegex(text, r'git tag[^\n]* -f')

    def test_exact_revision_and_native_attestation(self):
        text = WORKFLOW.read_text()
        self.assertIn("needs.resolve.outputs.sha", text)
        self.assertIn("actions/attest@", text)
        self.assertIn("attestations: write", text)
        self.assertIn("--verify-tag", text)


if __name__ == "__main__":
    unittest.main()
