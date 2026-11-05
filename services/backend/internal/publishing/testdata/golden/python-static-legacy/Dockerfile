FROM registry.example.com/base/shiny:1 AS site-build
USER 0
WORKDIR /app
COPY source/ /app/
RUN if [ -f requirements.txt ]; then python -m pip install --no-cache-dir -r requirements.txt; fi
RUN ["sh","-c","python build_html.py"]
RUN ["python","-c","import itertools, shutil, sys\nfrom pathlib import Path\nroot = Path.cwd().resolve()\nsource = root / sys.argv[1]\nresolved = source.resolve()\nif resolved == root or root not in resolved.parents:\n    raise SystemExit(\"Static output must stay inside the project directory\")\nif not source.is_dir() or not (source / \"index.html\").is_file():\n    raise SystemExit(\"Static build must produce index.html inside the configured output directory\")\nfor item in itertools.chain((source,), source.rglob(\"*\")):\n    if item.is_symlink() or not (item.is_file() or item.is_dir()):\n        raise SystemExit(\"Static output cannot contain links or special files\")\ntarget = Path(sys.argv[2])\nshutil.copytree(source, target)\nfor item in itertools.chain((target,), target.rglob(\"*\")):\n    item.chmod(0o755 if item.is_dir() else 0o644)\n","dist","/justcd-site"]

FROM registry.example.com/base/nginx:1 AS site-runtime
COPY --from=site-build /justcd-site/ /usr/share/nginx/html/
USER 1001
EXPOSE 8080
