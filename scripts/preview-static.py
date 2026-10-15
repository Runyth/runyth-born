#!/usr/bin/env python3
import http.server
import os
import subprocess
import sys
import threading
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
DIST = Path(os.environ.get("DIST_DIR", ROOT / "dist"))
HOST = "127.0.0.1"
PORT = int(os.environ.get("PORT", "8080"))
WATCH = (
    ROOT / "site",
    ROOT / "templates",
    ROOT / "internal",
    ROOT / "cmd" / "template-catalog",
    ROOT / "scripts" / "build-static.sh",
    ROOT / "VERSION",
)


def snapshot():
    stamps = []
    for path in WATCH:
        if path.is_file():
            stamps.append((path, path.stat().st_mtime_ns))
            continue
        if not path.is_dir():
            continue
        for file in path.rglob("*"):
            if file.is_file() and not file.is_symlink():
                try:
                    stamps.append((file, file.stat().st_mtime_ns))
                except OSError:
                    continue
    return tuple(sorted(stamps))


def build():
    subprocess.run(
        ["bash", str(ROOT / "scripts" / "build-static.sh"), str(DIST)],
        cwd=ROOT,
        check=True,
    )


class Handler(http.server.SimpleHTTPRequestHandler):
    extensions_map = {
        **http.server.SimpleHTTPRequestHandler.extensions_map,
        ".js": "text/javascript",
        ".mjs": "text/javascript",
        ".json": "application/json",
        ".css": "text/css",
        ".html": "text/html",
        ".txt": "text/plain",
    }

    def __init__(self, *args, **kwargs):
        super().__init__(*args, directory=str(DIST), **kwargs)

    def end_headers(self):
        self.send_header("Cache-Control", "no-cache")
        super().end_headers()


def serve():
    http.server.ThreadingHTTPServer.allow_reuse_address = True
    server = http.server.ThreadingHTTPServer((HOST, PORT), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server


def main():
    build()
    try:
        server = serve()
    except OSError as error:
        print(f"Could not listen on http://{HOST}:{PORT}/: {error}", file=sys.stderr)
        return 1
    print(f"Preview: http://{HOST}:{PORT}/", flush=True)
    print("Edits under site/ or templates/ rebuild this preview. Refresh the browser to see them. Ctrl+C stops the server.", flush=True)
    seen = snapshot()
    try:
        while True:
            time.sleep(0.4)
            current = snapshot()
            if current == seen:
                continue
            time.sleep(0.15)
            seen = snapshot()
            try:
                build()
            except subprocess.CalledProcessError:
                print("Rebuild failed; the previous preview is still being served.", file=sys.stderr)
                continue
            print("Rebuilt preview.", flush=True)
    except KeyboardInterrupt:
        print("\nStopped.", flush=True)
        server.shutdown()
        return 0


if __name__ == "__main__":
    raise SystemExit(main())
