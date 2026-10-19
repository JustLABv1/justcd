#!/usr/bin/env python3
"""Stage, validate, and package release charts (requires PyYAML and Helm)."""

import argparse
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

import yaml


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("tag", help="Release tag, for example v1.2.3")
    parser.add_argument("--destination", type=Path, default=Path("dist/charts"))
    args = parser.parse_args()
    if not re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", args.tag):
        parser.error("expected a stable release tag such as v1.2.3")

    version = args.tag[1:]
    root = Path(__file__).resolve().parents[1]
    destination = args.destination.resolve()
    destination.mkdir(parents=True, exist_ok=True)
    image_repository = "ghcr.io/justlabv1/justcd"
    with tempfile.TemporaryDirectory(prefix="justcd-charts-") as staging:
        for name in ("justcd", "justcd-agent"):
            chart = Path(staging) / name
            shutil.copytree(root / "charts" / name, chart)
            values_path = chart / "values.yaml"
            values = yaml.safe_load(values_path.read_text())
            if name == "justcd":
                for service in ("frontend", "backend", "docs"):
                    values[service]["image"].update(
                        repository=image_repository, tag=f"{service}-{args.tag}"
                    )
                validation = [
                    "--set", "publicUrl=https://justcd.example.com",
                    "--set", "backend.existingSecret=justcd-config",
                    "--set", "docs.enabled=true",
                ]
            else:
                values["image"].update(repository=image_repository, tag=f"agent-{args.tag}")
                validation = [
                    "--set", "serverUrl=https://justcd.example.com",
                    "--set", "enrollmentSecret=justcd-enrollment",
                    "--set", "profiles[0].name=default",
                    "--set", "profiles[0].workspaceIds[0]=release-check",
                    "--set", "profiles[0].namespaces[0]=workloads",
                ]
            values_path.write_text(yaml.safe_dump(values, sort_keys=False))
            metadata_path = chart / "Chart.yaml"
            metadata = yaml.safe_load(metadata_path.read_text())
            metadata.update(version=version, appVersion=version)
            metadata_path.write_text(yaml.safe_dump(metadata, sort_keys=False))
            subprocess.run(["helm", "lint", "--strict", str(chart), *validation], check=True)
            subprocess.run(
                ["helm", "template", "release-check", str(chart), *validation],
                check=True, stdout=subprocess.DEVNULL,
            )
            subprocess.run(
                ["helm", "package", str(chart), "--destination", str(destination)], check=True
            )


if __name__ == "__main__":
    main()
