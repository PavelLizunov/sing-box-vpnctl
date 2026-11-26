import json
import unittest
from unittest.mock import patch

from check_transport_upstreams import collect


class UpstreamReportTests(unittest.TestCase):
    def test_report_tracks_changes_including_paths_renamed_out_of_scope(self):
        baseline, head = "a" * 40, "b" * 40
        entry = {"repo": "example/project", "refs": {"main": baseline}, "release": "v1",
                 "issues": {"42": "earlier"}, "paths": ["protocol/*"]}
        rename = {"status": "renamed", "filename": "legacy/client.go", "previous_filename": "protocol/client.go"}
        requests = [
            [{"sha": head}],
            {"status": "ahead", "ahead_by": 2, "behind_by": 0,
             "files": [rename, {"status": "modified", "filename": "README.md"}]},
            {"tag_name": "v2"},
            {"state": "closed", "updated_at": "today"},
            [{"full_name": "example/project"}, {"full_name": "example/new"}],
        ]
        with patch("check_transport_upstreams.github", side_effect=requests):
            report = collect({"repositories": [entry], "discover_organizations": ["example"]})
        self.assertEqual(report["status"], "complete")
        row = report["repositories"][0]
        self.assertEqual(row["refs"]["main"]["watched_changes"], [rename])
        self.assertFalse(row["refs"]["main"]["paths_may_be_incomplete"])
        self.assertTrue(row["changed"] and row["release"]["changed"] and row["issues"]["42"]["changed"])
        self.assertEqual(report["untracked_from_30_recently_pushed"]["example"], ["example/new"])

    def test_auth_header_is_normalized_and_never_part_of_a_report(self):
        entry = {"repo": "example/project", "refs": {"HEAD": "a" * 40}, "release": None, "paths": []}
        manifest = {"repositories": [entry], "discover_organizations": []}

        def reject(request, timeout):
            self.assertEqual(request.get_header("Authorization"), "Bearer unit-test-token")
            raise ValueError("invalid header: Bearer unit-test-token")

        with patch.dict("os.environ", {"GITHUB_TOKEN": "unit-test-token\n"}), patch("urllib.request.urlopen", reject):
            report = collect(manifest)
        self.assertEqual(report["status"], "partial")
        self.assertEqual(report["repositories"][0]["errors"]["ref:HEAD"], "ValueError")
        self.assertNotIn("unit-test-token", json.dumps(report))

    def test_empty_reference_is_an_error_even_without_a_release(self):
        entry = {"repo": "example/empty", "refs": {"HEAD": "b" * 40}, "release": None, "paths": []}
        with patch("check_transport_upstreams.github", side_effect=[[], None]):
            result = collect({"repositories": [entry], "discover_organizations": []})
        self.assertEqual(result["status"], "partial")
        self.assertIsNone(result["repositories"][0]["release"]["tag"])


if __name__ == "__main__":
    unittest.main()
