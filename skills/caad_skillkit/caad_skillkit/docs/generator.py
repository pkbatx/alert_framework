from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Dict, Iterable, List

DOC_BEGIN = "<!-- CAAD_DOCS_BEGIN -->"
DOC_END = "<!-- CAAD_DOCS_END -->"


@dataclass(frozen=True)
class DocChange:
    path: Path
    changed: bool
    created: bool


def _find_repo_root() -> Path:
    start = Path.cwd().resolve()
    for parent in [start] + list(start.parents):
        if (parent / "contracts").is_dir():
            return parent
        if (parent / ".git").exists():
            return parent
    return start


def _managed_block(content: str) -> str:
    return f"{DOC_BEGIN}\n{content.strip()}\n{DOC_END}\n"


def _apply_managed_section(existing: str, content: str) -> str:
    block = _managed_block(content)
    if DOC_BEGIN in existing and DOC_END in existing:
        pre, _ = existing.split(DOC_BEGIN, 1)
        _, post = existing.split(DOC_END, 1)
        return f"{pre}{block}{post.lstrip()}"
    existing = existing.rstrip()
    if not existing:
        return block
    return f"{existing}\n\n{block}"


def _write_if_changed(path: Path, content: str, write: bool) -> DocChange:
    existed = path.exists()
    existing = path.read_text() if existed else ""
    updated = _apply_managed_section(existing, content)
    changed = updated != existing
    if write and changed:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(updated)
    return DocChange(path=path, changed=changed, created=not existed)


ALLOWLIST = {
    "contracts",
    "core",
    "worker",
    "runtime",
    "models",
    "skills",
    "scripts",
    "docker",
    "backend",
    "config",
    "web",
}

IGNORED_DIRS = {
    ".git",
    ".venv",
    "__pycache__",
    "node_modules",
    ".gocache",
    ".nx",
    ".pytest_cache",
    ".mypy_cache",
    ".ruff_cache",
}

SOURCE_EXTENSIONS = {".py", ".go", ".ts", ".tsx", ".js", ".jsx"}

CATEGORY_MAP = {
    "contracts": "Runtime",
    "core": "Runtime",
    "worker": "Runtime",
    "runtime": "Runtime",
    "models": "Runtime",
    "skills": "Runtime",
    "skills/caad_skillkit": "Runtime",
    "scripts": "Tooling",
    "docker": "Tooling",
    "backend": "Legacy",
    "config": "Legacy",
    "web": "Legacy",
}

BOUNDARY_META = {
    "contracts": {
        "role": "JSON schemas for call artifacts and AI outputs.",
        "must_not": "Generated data or runtime state.",
        "depends": "Skillkit, worker, and store validators.",
        "runtime": "yes",
        "delete": "no",
    },
    "core": {
        "role": "File-based artifact store helpers (paths, atomic writes).",
        "must_not": "Ingestion logic or AI clients.",
        "depends": "Worker and CLI store commands.",
        "runtime": "yes",
        "delete": "no",
    },
    "worker": {
        "role": "Polling ingestion worker (audio → artifacts).",
        "must_not": "UI, API, or DB migrations.",
        "depends": "Skillkit AI + core store.",
        "runtime": "yes",
        "delete": "no",
    },
    "runtime": {
        "role": "Generated call artifacts and call records (gitignored).",
        "must_not": "Source code.",
        "depends": "Produced by worker, read by store/CLI.",
        "runtime": "yes",
        "delete": "yes (data loss)",
    },
    "models": {
        "role": "LocalAI model configs and binaries (binaries gitignored).",
        "must_not": "Secrets or source code.",
        "depends": "LocalAI and skillkit when AI_BACKEND=localai.",
        "runtime": "conditional",
        "delete": "no if using LocalAI",
    },
    "skills": {
        "role": "Repo-local skillkit and `caad` CLI for AI, env, ops, and docs.",
        "must_not": "Runtime artifacts or call data.",
        "depends": "Worker imports and CLI users.",
        "runtime": "yes",
        "delete": "no",
    },
    "skills/caad_skillkit": {
        "role": "Skillkit implementation package.",
        "must_not": "Runtime artifacts or call data.",
        "depends": "CLI and worker imports.",
        "runtime": "yes",
        "delete": "no",
    },
    "scripts": {
        "role": "Local helper scripts (smoke, dev reset).",
        "must_not": "Runtime logic.",
        "depends": "Humans and CI.",
        "runtime": "no",
        "delete": "yes",
    },
    "docker": {
        "role": "Docker helper assets referenced by Dockerfile/compose.",
        "must_not": "Runtime data.",
        "depends": "Docker builds only.",
        "runtime": "no",
        "delete": "no if using Docker",
    },
    "backend": {
        "role": "Legacy Go backend (pre-Phase 0–2).",
        "must_not": "Phase 0–2 pipeline logic.",
        "depends": "Legacy runtime only.",
        "runtime": "no",
        "delete": "no (legacy)",
    },
    "config": {
        "role": "Legacy Go configuration and prompts.",
        "must_not": "Phase 0–2 pipeline logic.",
        "depends": "Legacy runtime only.",
        "runtime": "no",
        "delete": "no (legacy)",
    },
    "web": {
        "role": "Legacy Next.js UI (not used in Phase 0–2).",
        "must_not": "Phase 0–2 pipeline logic.",
        "depends": "Legacy runtime only.",
        "runtime": "no",
        "delete": "no (legacy)",
    },
}


def _list_top_level_dirs(root: Path) -> List[Path]:
    dirs = []
    for entry in root.iterdir():
        if not entry.is_dir():
            continue
        if entry.name in IGNORED_DIRS:
            continue
        if entry.name.startswith("."):
            continue
        dirs.append(entry)
    return dirs


def _boundary_targets(root: Path) -> Dict[str, Path]:
    targets: Dict[str, Path] = {}
    for entry in _list_top_level_dirs(root):
        if entry.name in ALLOWLIST:
            targets[entry.name] = entry
    subsystem = root / "skills" / "caad_skillkit"
    if subsystem.is_dir():
        targets["skills/caad_skillkit"] = subsystem
    return targets


def _other_directories(root: Path) -> List[str]:
    allow = set(ALLOWLIST)
    names = [entry.name for entry in _list_top_level_dirs(root) if entry.name not in allow]
    return sorted(names)


def _source_files_count(path: Path) -> int:
    count = 0
    for entry in path.rglob("*"):
        if entry.is_file() and entry.suffix in SOURCE_EXTENSIONS:
            count += 1
    return count


def _readme_marked_deprecated(path: Path) -> bool:
    readme = path / "README.md"
    if not readme.exists():
        return False
    text = readme.read_text().lower()
    return "status: deprecated" in text or "[deprecated]" in text


def _candidate_reference_files(root: Path) -> List[Path]:
    files: List[Path] = []
    for name in ["docker-compose.yml", "docker-compose.dev.yml", "compose.yml"]:
        candidate = root / name
        if candidate.exists():
            files.append(candidate)
    for pattern in ["scripts/**/*.sh", "scripts/**/*.py", "skills/caad_skillkit/**/*.py", "worker/**/*.py", "core/**/*.py"]:
        files.extend(root.glob(pattern))
    return sorted({file for file in files})


def _tokens_for_boundary(name: str) -> List[str]:
    tokens = []
    base = name.split("/")[-1]
    tokens.append(f"{name}/")
    tokens.append(f"./{name}")
    tokens.append(f"../{name}")
    tokens.append(f"import {base}")
    tokens.append(f"from {base}")
    if name == "skills/caad_skillkit":
        tokens.append("caad_skillkit")
    return tokens


def _scan_references(root: Path, boundary_names: List[str]) -> Dict[str, bool]:
    files = _candidate_reference_files(root)
    tokens_map = {name: _tokens_for_boundary(name) for name in boundary_names}
    referenced = {name: False for name in boundary_names}
    for file in files:
        if file.stat().st_size > 2 * 1024 * 1024:
            continue
        text = file.read_text()
        for name, tokens in tokens_map.items():
            if referenced[name]:
                continue
            if any(token in text for token in tokens):
                referenced[name] = True
    return referenced


def _classify_boundaries(root: Path, boundaries: Dict[str, Path]) -> Dict[str, Dict[str, object]]:
    names = sorted(boundaries.keys())
    referenced = _scan_references(root, names)
    results: Dict[str, Dict[str, object]] = {}
    for name in names:
        path = boundaries[name]
        ref = referenced[name]
        source_files = _source_files_count(path)
        readme_deprecated = _readme_marked_deprecated(path)
        if not ref and readme_deprecated:
            status = "deprecated"
        elif not ref and source_files == 0:
            status = "stale"
        elif ref:
            status = "active"
        else:
            status = "unreferenced"
        results[name] = {
            "path": path,
            "referenced": ref,
            "source_files": source_files,
            "readme_deprecated": readme_deprecated,
            "status": status,
        }
    return results


def _boundary_block(
    name: str,
    info: Dict[str, object],
    description: str,
    must_not: str,
    depends: str,
    runtime_flag: str,
    delete_flag: str,
) -> List[str]:
    return [
        f"### `{name}/`",
        f"- Lives here: {description}",
        f"- Must not live here: {must_not}",
        f"- Depends on it: {depends}",
        f"- Runtime-critical: {runtime_flag}",
        f"- Safe to delete: {delete_flag}",
        f"- Evidence: referenced={info['referenced']}, source_files={info['source_files']}, readme_deprecated={info['readme_deprecated']}",
        "",
    ]


def build_structure_section() -> str:
    root = _find_repo_root()
    boundaries = _boundary_targets(root)
    classified = _classify_boundaries(root, boundaries)

    active = [name for name, info in classified.items() if info["status"] == "active"]
    unreferenced = [name for name, info in classified.items() if info["status"] == "unreferenced"]
    deprecated = [name for name, info in classified.items() if info["status"] == "deprecated"]
    stale = [name for name, info in classified.items() if info["status"] == "stale"]

    lines: List[str] = [
        "## Repository Structure (managed)",
        "",
        "Authoritative boundary map generated by `caad docs`.",
        "",
        "### Active boundaries",
    ]

    for category in ["Runtime", "Tooling", "Legacy"]:
        category_names = [name for name in active if CATEGORY_MAP.get(name) == category]
        if not category_names:
            continue
        lines.append(f"#### {category}")
        for name in category_names:
            meta = BOUNDARY_META.get(name, {})
            info = classified[name]
            lines.extend(
                _boundary_block(
                    name,
                    info,
                    meta.get("role", "Boundary directory."),
                    meta.get("must_not", "N/A"),
                    meta.get("depends", "N/A"),
                    meta.get("runtime", "unknown"),
                    meta.get("delete", "unknown"),
                )
            )

    if unreferenced:
        lines.append("### Unreferenced boundaries (source present)")
        for name in unreferenced:
            meta = BOUNDARY_META.get(name, {})
            info = classified[name]
            lines.extend(
                _boundary_block(
                    name,
                    info,
                    meta.get("role", "Boundary directory."),
                    meta.get("must_not", "N/A"),
                    meta.get("depends", "N/A"),
                    meta.get("runtime", "unknown"),
                    meta.get("delete", "unknown"),
                )
            )

    if deprecated:
        lines.append("### Deprecated boundaries")
        for name in deprecated:
            meta = BOUNDARY_META.get(name, {})
            info = classified[name]
            lines.extend(
                _boundary_block(
                    name,
                    info,
                    meta.get("role", "Boundary directory."),
                    meta.get("must_not", "N/A"),
                    meta.get("depends", "N/A"),
                    meta.get("runtime", "unknown"),
                    meta.get("delete", "unknown"),
                )
            )

    if stale:
        lines.append("### Stale boundaries")
        for name in stale:
            meta = BOUNDARY_META.get(name, {})
            info = classified[name]
            lines.extend(
                _boundary_block(
                    name,
                    info,
                    meta.get("role", "Boundary directory."),
                    meta.get("must_not", "N/A"),
                    meta.get("depends", "N/A"),
                    meta.get("runtime", "unknown"),
                    meta.get("delete", "unknown"),
                )
            )

    other_dirs = _other_directories(root)
    lines.append("### Other directories")
    if other_dirs:
        for name in other_dirs:
            lines.append(f"- `{name}/`")
    else:
        lines.append("- none")

    return "\n".join(lines)


def build_readme_section() -> str:
    return "\n".join(
        [
            "## CAAD System Overview (managed)",
            "",
            "Current system behavior (Phases 0–2):",
            "",
            "- Polling worker ingests audio from `CALLS_DIR` and writes call artifacts",
            "- File-based artifact store under `runtime/calls/<call_id>/`",
            "- Contracts in `contracts/` define every JSON artifact",
            "- Skillkit (`caad` CLI) provides AI, validation, and doc tooling",
            "",
            "There is no UI or database in the Phase 0–2 pipeline; legacy Go/Next assets remain but are inactive.",
            "See `STRUCTURE.md` for boundary-level details.",
        ]
    )


def build_boundaries() -> Dict[str, str]:
    root = _find_repo_root()
    boundaries = {name: path for name, path in _boundary_targets(root).items() if "/" not in name}
    classified = _classify_boundaries(root, boundaries)
    results: Dict[str, str] = {}
    for name in sorted(boundaries.keys()):
        info = classified[name]
        meta = BOUNDARY_META.get(name, {})
        results[name] = "\n".join(
            [
                f"## `{name}/` boundary (managed)",
                "",
                f"Role: {meta.get('role', 'Boundary directory.')}",
                f"Must not live here: {meta.get('must_not', 'N/A')}",
                f"Depends on it: {meta.get('depends', 'N/A')}",
                f"Runtime-critical: {meta.get('runtime', 'unknown')}",
                f"Safe to delete: {meta.get('delete', 'unknown')}",
                f"Status: {info.get('status', 'unknown')}",
                f"Evidence: referenced={info.get('referenced')}, source_files={info.get('source_files')}, readme_deprecated={info.get('readme_deprecated')}",
                "",
                "Managed content is appended below; do not edit between markers.",
            ]
        )
    return results


def docs_structure(write: bool) -> List[DocChange]:
    root = _find_repo_root()
    target = root / "STRUCTURE.md"
    return [_write_if_changed(target, build_structure_section(), write)]


def docs_readme(write: bool) -> List[DocChange]:
    root = _find_repo_root()
    target = root / "README.md"
    return [_write_if_changed(target, build_readme_section(), write)]


def docs_boundaries(write: bool) -> List[DocChange]:
    root = _find_repo_root()
    changes: List[DocChange] = []
    for name, content in build_boundaries().items():
        target = root / name / "README.md"
        changes.append(_write_if_changed(target, content, write))
    return changes


def _summarize(changes: Iterable[DocChange]) -> Dict[str, List[str]]:
    changed = [str(change.path) for change in changes if change.changed]
    created = [str(change.path) for change in changes if change.created]
    return {"changed": sorted(changed), "created": sorted(created)}


def docs_all(write: bool) -> Dict[str, List[str]]:
    changes: List[DocChange] = []
    changes.extend(docs_structure(write))
    changes.extend(docs_readme(write))
    changes.extend(docs_boundaries(write))
    return _summarize(changes)


def docs_validate() -> Dict[str, List[str]]:
    summary = docs_all(write=False)
    if summary["changed"]:
        raise RuntimeError("documentation is out of date")
    return summary
