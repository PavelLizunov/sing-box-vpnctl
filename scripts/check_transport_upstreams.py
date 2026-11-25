"""Report changes since the maintained transport research baselines."""

import argparse
import datetime
import fnmatch
import http.client
import json
import os
from pathlib import Path
import re
import urllib.error
import urllib.parse
import urllib.request


ROOT = Path(__file__).resolve().parents[1]
COMPARE_FILE_LIMIT = 300


REQUEST_ERRORS = (OSError, http.client.HTTPException, ValueError, LookupError, TypeError)


def error_label(exc):
    if isinstance(exc, urllib.error.HTTPError):
        return f"GitHub HTTP {exc.code}"
    return type(exc).__name__


def github(path, *, optional=False):
    headers = {
        "User-Agent": "sing-box-vpnctl-upstream-watch",
        "Accept": "application/vnd.github+json",
        "X-GitHub-Api-Version": "2022-11-28",
    }
    request = urllib.request.Request("https://api.github.com" + path, headers=headers)
    if token := os.environ.get("GITHUB_TOKEN", "").strip():
        request.add_unredirected_header("Authorization", "Bearer " + token)
    try:
        with urllib.request.urlopen(request, timeout=15) as response:
            return json.load(response)
    except urllib.error.HTTPError as exc:
        if optional and exc.code == 404:
            return None
        raise


def compare_ref(repo, ref, baseline, patterns, result):
    rows = github(f"/repos/{repo}/commits?" + urllib.parse.urlencode({"sha": ref, "per_page": 1}))
    head = rows[0]["sha"]
    if not all(re.fullmatch(r"[0-9a-f]{40}", sha) for sha in (head, baseline)):
        raise ValueError("expected a full commit SHA")
    result.update(head=head, changed=head != baseline)
    if head == baseline:
        return
    comparison = f"{baseline}...{head}"
    result["compare_url"] = f"https://github.com/{repo}/compare/{comparison}"
    diff = github(f"/repos/{repo}/compare/{comparison}?per_page=1")
    files = diff["files"]
    changed_paths = []
    for file in files:
        names = [file["filename"]]
        if "previous_filename" in file:
            names.append(file["previous_filename"])
        if any(fnmatch.fnmatchcase(name, pattern) for name in names for pattern in patterns):
            changed_paths.append({key: file[key] for key in ("status", "filename", "previous_filename") if key in file})
    result.update(
        relationship=diff["status"],
        ahead_by=diff["ahead_by"],
        behind_by=diff["behind_by"],
        watched_changes=changed_paths,
        paths_may_be_incomplete=len(files) >= COMPARE_FILE_LIMIT or diff["behind_by"] > 0,
    )


def inspect_repository(entry):
    repo = entry["repo"]
    result = {"repo": repo, "refs": {}, "issues": {}, "errors": {}}
    for ref, baseline in entry["refs"].items():
        branch = {"baseline": baseline}
        result["refs"][ref] = branch
        try:
            compare_ref(repo, ref, baseline, entry["paths"], branch)
        except (OSError, http.client.HTTPException, ValueError, KeyError, IndexError, TypeError) as exc:
            result["errors"]["ref:" + ref] = error_label(exc)

    try:
        latest = github(f"/repos/{repo}/releases/latest", optional=True)
        tag = latest["tag_name"] if latest is not None else None
        result["release"] = {"baseline": entry["release"], "tag": tag, "changed": tag != entry["release"]}
    except REQUEST_ERRORS as exc:
        result["errors"]["release"] = error_label(exc)

    for number, reviewed_at in entry.get("issues", {}).items():
        try:
            issue = github(f"/repos/{repo}/issues/{number}")
            result["issues"][number] = {"state": issue["state"], "updated_at": issue["updated_at"],
                                        "changed": issue["updated_at"] != reviewed_at}
        except REQUEST_ERRORS as exc:
            result["errors"]["issue:" + number] = error_label(exc)

    observations = [*result["refs"].values(), *result["issues"].values(), result.get("release", {})]
    result["changed"] = any(item.get("changed") for item in observations)
    return result


def collect(manifest):
    repositories = [inspect_repository(entry) for entry in manifest["repositories"]]
    known = {row["repo"].lower() for row in repositories}
    discovery = {}
    errors = {}
    for org in manifest["discover_organizations"]:
        try:
            recent = github(f"/orgs/{org}/repos?type=public&sort=pushed&direction=desc&per_page=30")
            discovery[org] = [repo["full_name"] for repo in recent if repo["full_name"].lower() not in known]
        except REQUEST_ERRORS as exc:
            errors[org] = error_label(exc)

    return {
        "checked_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "status": "partial" if errors or any(row["errors"] for row in repositories) else "complete",
        "repositories": repositories,
        "untracked_from_30_recently_pushed": discovery,
        "discovery_errors": errors,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, default=ROOT / "docs/vpnctl/upstreams.json")
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    report = collect(json.loads(args.manifest.read_text(encoding="utf-8")))
    args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"{report['status']}, {len(report['repositories'])} repositories; report {args.output}")
    return int(report["status"] == "partial")


if __name__ == "__main__":
    raise SystemExit(main())
