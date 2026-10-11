#!/usr/bin/env python3
"""Watch the Java SDK for the loss of its public Conductor connector.

Relay derives the Conductor executor WebSocket protocol from the public source
of the DBOS Transact SDKs. Go, Python, and TypeScript have all moved their
connector out of the public package on main, so the Java SDK is the only public
source of the wire protocol left. This watch clones that repository and fails
when the connector package is no longer there.

It needs network access, so it is deliberately not part of `make check` or any
offline test. Run it on a schedule, or by hand before a release:

    make watch/java-connector

Exit status is 0 when the connector is present at the recorded baseline and at
the current ref, 1 when the watch itself fails, and 2 when the check could not
run at all (git missing, clone failed).

The baseline constants below were observed 2026-10-11. They are checked on
every run, so a mistyped commit, path, or count fails the watch instead of
quietly passing against nothing.
"""

from __future__ import annotations

import argparse
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

REPOSITORY = "https://github.com/dbos-inc/dbos-transact-java.git"
BASELINE_COMMIT = "85b3f0ed0458b72358fb7554bc20931bd61e7897"
PACKAGE_PATH = "transact/src/main/java/dev/dbos/transact/conductor/"
EXPECTED_FILES = 64
OBSERVED = "2026-10-11"
REF = "origin/main"

DOCS_PAGE = "docs/usage/supported-sdks.md"

ACTION_NEXT_STEPS = """\
  action: this is the last public source of the wire protocol. Record the loss,
          refresh the protocol pins in docs/discovery/provenance.md, re-derive
          docs/protocol/executor-ws.md from a permitted source or mark the
          affected claims as unverifiable, and add a Java ceiling to
          tests/verifysdk/sdk_ceiling_test.go. See {page}."""


def run_git(workdir: Path, *args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["git", "-C", str(workdir), *args],
        capture_output=True,
        text=True,
        check=False,
    )


def files_under(workdir: Path, revision: str, package: str) -> tuple[list[str], str]:
    """List the files under a package path at a revision.

    Returns the paths and an error string. An error means git could not answer,
    which is a different problem from a path that is simply not there.
    """
    result = run_git(workdir, "ls-tree", "-r", "--name-only", revision, "--", package)
    if result.returncode != 0:
        detail = (result.stderr or result.stdout).strip().splitlines()
        return [], detail[-1] if detail else f"git exited {result.returncode}"
    return [line for line in result.stdout.splitlines() if line.strip()], ""


def resolve_revision(workdir: Path, revision: str) -> str | None:
    result = run_git(workdir, "rev-parse", "--verify", f"{revision}^{{commit}}")
    if result.returncode != 0:
        return None
    return result.stdout.strip()


def parse_args(argv: list[str] | None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Fail when the Java SDK stops publishing its public Conductor connector.",
    )
    parser.add_argument("--repo", default=REPOSITORY, help="Java SDK repository to clone")
    parser.add_argument(
        "--baseline",
        default=BASELINE_COMMIT,
        help="last-known-good commit of the connector package",
    )
    parser.add_argument("--package", default=PACKAGE_PATH, help="connector package path")
    parser.add_argument(
        "--expect-files",
        type=int,
        default=EXPECTED_FILES,
        help="file count recorded at the baseline",
    )
    parser.add_argument("--ref", default=REF, help="ref to compare the baseline against")
    parser.add_argument("--verbose", action="store_true", help="list every file found")
    return parser.parse_args(argv)


def fail(message: str) -> int:
    """Report a failure on stderr after the stdout context, which is buffered."""
    sys.stdout.flush()
    print(f"watch/java-connector: FAIL\n  {message}", file=sys.stderr)
    return 1


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)

    if shutil.which("git") is None:
        print("watch/java-connector: cannot run, git is not installed", file=sys.stderr)
        return 2

    workdir = Path(tempfile.mkdtemp(prefix="relay-java-connector-watch-"))
    try:
        clone = subprocess.run(
            ["git", "clone", "--quiet", "--no-checkout", args.repo, str(workdir)],
            capture_output=True,
            text=True,
            check=False,
        )
        if clone.returncode != 0:
            detail = (clone.stderr or clone.stdout).strip().splitlines()
            tail = detail[-1] if detail else "no output"
            print(f"watch/java-connector: cannot clone {args.repo}: {tail}", file=sys.stderr)
            return 2

        baseline_revision = resolve_revision(workdir, args.baseline)
        if baseline_revision is None:
            return fail(
                f"the recorded baseline commit {args.baseline} is not in {args.repo}.\n"
                "  A rewritten history would break this watch, so the baseline has to be\n"
                "  re-recorded from a permitted public source before the check can mean\n"
                "  anything."
            )

        baseline_files, baseline_error = files_under(workdir, baseline_revision, args.package)
        if baseline_error:
            return fail(
                f"cannot list {args.package} at the recorded baseline {baseline_revision}:\n"
                f"  {baseline_error}"
            )
        if not baseline_files:
            return fail(
                f"no files under {args.package} at the recorded baseline {args.baseline}.\n"
                "  The recorded baseline is wrong or the package moved, so this watch can\n"
                "  no longer tell a loss from a bad path."
            )
        if len(baseline_files) < args.expect_files:
            return fail(
                f"the recorded baseline {baseline_revision} carries {len(baseline_files)} files\n"
                f"  under {args.package}, fewer than the {args.expect_files} recorded on\n"
                f"  {OBSERVED}. Re-check the baseline against a permitted public source before\n"
                "  trusting this watch."
            )

        current_revision = resolve_revision(workdir, args.ref)
        if current_revision is None:
            return fail(
                f"cannot resolve {args.ref} in {args.repo}; the baseline package is intact\n"
                f"  ({len(baseline_files)} files), but the current state is unknown."
            )

        current_files, current_error = files_under(workdir, current_revision, args.package)
        if current_error:
            return fail(
                f"cannot list {args.package} at {args.ref} {current_revision}:\n"
                f"  {current_error}"
            )
        print(
            f"watch/java-connector: {args.repo}\n"
            f"  baseline {baseline_revision[:12]} ({OBSERVED}): {len(baseline_files)} files under {args.package}\n"
            f"  {args.ref} {current_revision[:12]}: {len(current_files)} files"
        )

        if not current_files:
            return fail(
                f"{args.ref} of {args.repo} no longer publishes {args.package}.\n"
                f"  the baseline {baseline_revision[:12]} observed {OBSERVED} still had "
                f"{len(baseline_files)} files.\n"
                + ACTION_NEXT_STEPS.format(page=DOCS_PAGE)
            )

        if len(current_files) < len(baseline_files):
            return fail(
                f"{args.ref} of {args.repo} publishes {len(current_files)} files under\n"
                f"  {args.package}, fewer than the {len(baseline_files)} at the baseline\n"
                f"  {baseline_revision[:12]} on {OBSERVED}. The connector is being moved out\n"
                "  of the public package.\n"
                + ACTION_NEXT_STEPS.format(page=DOCS_PAGE)
            )

        if len(current_files) > len(baseline_files):
            print(
                f"  note: {len(current_files)} files is more than the {len(baseline_files)} at the\n"
                f"  baseline; update EXPECTED_FILES once the growth is confirmed to be the\n"
                "  connector rather than something else"
            )

        if args.verbose:
            for path in current_files:
                print(f"  {path}")

        print("watch/java-connector: ok, the public Conductor connector is still published")
        return 0
    finally:
        shutil.rmtree(workdir, ignore_errors=True)


if __name__ == "__main__":
    sys.exit(main())
