#!/usr/bin/env python3
"""Validate the deployable static site without third-party Python packages."""

from __future__ import annotations

import json
import sys
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import urlparse, unquote
import re


SOURCE_CITATION = "https://github.com/Runyth/runyth-born"


class ReferenceParser(HTMLParser):
  def __init__(self) -> None:
    super().__init__()
    self.references: list[tuple[str, str]] = []
    self.base = ""

  def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
    values = dict(attrs)
    if tag == "base":
      self.base = values.get("href") or ""
    if tag in {"a", "link"} and values.get("href"):
      self.references.append((tag, values["href"] or ""))
    if tag in {"script", "img"} and values.get("src"):
      self.references.append((tag, values["src"] or ""))


def fail(message: str) -> None:
  raise SystemExit(f"static validation: {message}")


def validate_html(root: Path, path: Path) -> None:
  parser = ReferenceParser()
  parser.feed(path.read_text(encoding="utf-8"))
  if parser.base and (path.name != "404.html" or not parser.base.startswith("/") or parser.base.startswith("//")):
    fail(f"unexpected document base: {parser.base}")
  for tag, reference in parser.references:
    if not reference or reference.startswith(("#", "mailto:", "data:")):
      continue
    parsed = urlparse(reference)
    if parsed.scheme or parsed.netloc:
      if tag == "a" and reference == SOURCE_CITATION:
        continue
      fail(f"external runtime reference in {path.relative_to(root)}: {reference}")
    if parsed.path.startswith("/"):
      fail(f"root-relative reference breaks project Pages sites: {reference}")
    target = (path.parent / unquote(parsed.path)).resolve()
    try:
      target.relative_to(root.resolve())
    except ValueError:
      fail(f"reference escapes static root in {path.relative_to(root)}: {reference}")
    if parsed.path.endswith("/"):
      target /= "index.html"
    if not target.exists():
      fail(f"missing reference from {path.relative_to(root)}: {reference}")


def main() -> None:
  if len(sys.argv) != 2:
    fail("usage: validate-static.py DIST")
  root = Path(sys.argv[1]).resolve()
  if not root.is_dir():
    fail(f"not a directory: {root}")

  required = [
    root / "index.html",
    root / "wizard.html",
    root / "404.html",
    root / ".nojekyll",
    root / "assets" / "runtime.js",
    root / "license.txt",
    root / "templates" / "catalog.json",
  ]
  for path in required:
    if not path.exists():
      fail(f"missing required file: {path.relative_to(root)}")

  catalog = json.loads((root / "templates" / "catalog.json").read_text(encoding="utf-8"))
  if catalog.get("schema_version") != 1 or not isinstance(catalog.get("templates"), list):
    fail("templates/catalog.json has an unsupported schema")
  catalog_filenames: set[str] = set()
  for entry in catalog["templates"]:
    filename = entry.get("filename", "")
    if Path(filename).name != filename:
      fail(f"unsafe template filename in catalog: {filename!r}")
    if not (root / "templates" / filename).is_file():
      fail(f"catalog references missing template: {filename}")
    if entry.get("question_count", 0) < 1:
      fail(f"catalog template has no questions: {filename}")
    catalog_filenames.add(filename)
    compiled_path = (root / unquote(entry.get("url", ""))).resolve()
    if compiled_path.parent != root / "templates" or not compiled_path.is_file():
      fail(f"missing or unsafe compiled template: {entry.get('url')}")
    compiled = json.loads(compiled_path.read_text(encoding="utf-8"))
    if compiled.get("schema_version") != 1 or len(compiled.get("questions", [])) != entry["question_count"]:
      fail(f"compiled question count mismatch: {filename}")
    if compiled.get("fingerprint") != entry.get("fingerprint"):
      fail(f"compiled template fingerprint mismatch: {filename}")
    slots = [part["question"] for part in compiled.get("parts", []) if "question" in part]
    if slots != list(range(entry["question_count"])):
      fail(f"compiled answer slots are invalid: {filename}")
    catalog_filenames.add(compiled_path.name)

  published_templates = {
    path.name
    for path in (root / "templates").iterdir()
    if path.is_file() and path.name != "catalog.json"
  }
  if published_templates != catalog_filenames:
    fail("published template files do not exactly match the generated catalog")

  forbidden_suffixes = {".go", ".wasm", ".exe"}
  for path in root.rglob("*"):
    if path.is_symlink():
      fail(f"Pages artifacts cannot contain symbolic links: {path.relative_to(root)}")
    if path.is_file() and path.suffix in forbidden_suffixes:
      fail(f"runtime artifact unexpectedly contains source file: {path.relative_to(root)}")
    if path.name in {"wasm_exec.js", "Dockerfile", "compose.yaml", "compose.caddy.yaml"}:
      fail(f"runtime artifact unexpectedly contains server deployment file: {path.relative_to(root)}")

  for html in root.glob("*.html"):
    validate_html(root, html)

  for script in (root / "assets").glob("*.js"):
    source = script.read_text(encoding="utf-8")
    for reference in re.findall(r'from\s+["\']([^"\']+)["\']', source):
      if not reference.startswith("./") or not (script.parent / reference).is_file():
        fail(f"missing or external module in {script.name}: {reference}")
    if re.search(r'WebAssembly|wasm_exec|https?://(?!example\.invalid)', source):
      fail(f"non-static or external runtime dependency in {script.name}")

  print(f"Static site validation passed: {root}")


if __name__ == "__main__":
  main()
