"""Namespace fixture policy; no root, network mutations or binaries required."""
import importlib.util
from pathlib import Path
from types import SimpleNamespace
import unittest
from unittest.mock import call, patch

spec = importlib.util.spec_from_file_location("interop", Path(__file__).with_name("awg_official_interop.py"))
interop = importlib.util.module_from_spec(spec)
spec.loader.exec_module(interop)


class NetworkFixtureTests(unittest.TestCase):
    def setUp(self):
        for target, value in (("net_inode", 100), ("os.geteuid", 0),
                              ("os.stat", SimpleNamespace(st_ino=1)), ("shutil.which", "/usr/sbin/ip")):
            if "." in target:
                owner, attribute = target.split(".")
                patched = patch.object(getattr(interop, owner), attribute, return_value=value)
            else:
                patched = patch.object(interop, target, return_value=value)
            patched.start()
            self.addCleanup(patched.stop)

    def test_host_namespace_is_rejected_before_mutation(self):
        with patch.object(interop, "net_inode", return_value=1), patch.object(interop.Network, "change") as change:
            with self.assertRaisesRegex(interop.Failure, "namespace_guard"):
                interop.Network(True)
            change.assert_not_called()

    def test_existing_link_is_rejected_before_mutation(self):
        with patch.object(interop.Network, "inspect", return_value=[{"ifname": "lo"}, {"ifname": "eth0"}]), \
                patch.object(interop.Network, "change") as change:
            with self.assertRaisesRegex(interop.Failure, "namespace_not_fresh"):
                interop.Network(True)
            change.assert_not_called()

    def test_existing_default_route_is_rejected_before_mutation(self):
        with patch.object(interop.Network, "inspect", side_effect=[[{"ifname": "lo"}], [{"dev": "lo", "dst": "default"}]]), \
                patch.object(interop.Network, "change") as change:
            with self.assertRaisesRegex(interop.Failure, "namespace_not_fresh"):
                interop.Network(True)
            change.assert_not_called()

    def test_online_and_offline_fixture_differ_only_in_dummy_uplink(self):
        for offline in (False, True):
            with self.subTest(offline=offline), \
                    patch.object(interop.Network, "inspect", side_effect=[[{"ifname": "lo"}], [], []]), \
                    patch.object(interop.Network, "change") as change:
                interop.Network(True, offline)
                expected = [call("link", "set", "lo", "up")]
                if not offline:
                    expected += [call("link", "add", "awg-uplink", "type", "dummy"),
                                 call("address", "add", "192.0.2.1/32", "dev", "awg-uplink"),
                                 call("link", "set", "awg-uplink", "up"),
                                 call("route", "add", "default", "dev", "awg-uplink")]
                self.assertEqual(change.call_args_list, expected)


if __name__ == "__main__":
    unittest.main()
