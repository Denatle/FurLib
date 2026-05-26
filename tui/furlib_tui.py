from __future__ import annotations

import io
import json
from datetime import datetime, timezone
from pathlib import Path

import httpx
from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Container, Horizontal, Vertical
from textual.message import Message
from textual.reactive import reactive
from textual.screen import ModalScreen
from textual.widget import Widget
from textual.widgets import (
    Button,
    DataTable,
    Footer,
    Header,
    Input,
    Label,
    Static,
    TabbedContent,
    TabPane,
)
from textual import on, work

TUI_DIR       = Path(__file__).parent
PRESETS_FILE  = TUI_DIR / "presets.json"
DOWNLOADS_DIR = TUI_DIR / "downloads"

BASE_URL                    = "http://localhost:8080"
_SPINNER                    = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
_IMAGE_TYPES                = {"jpg", "jpeg", "png", "gif", "webp"}
_PREVIEW_MIN_TERMINAL_WIDTH = 120
_PREVIEW_WIDTH              = 42
_BAR_WIDTH                  = 28   # fixed progress bar character width


# ── Helpers ────────────────────────────────────────────────────────────────────

def _parse_source_tags(raw: str) -> dict:
    """Parse 'e621:order:score -animated; gelbooru:sort:score' into a dict."""
    result = {}
    for part in raw.split(";"):
        part = part.strip()
        if ":" not in part:
            continue
        source, _, tags_raw = part.partition(":")
        source = source.strip()
        tags   = tags_raw.strip().split()
        if source and tags:
            result[source] = tags
    return result


def _fmt_source_tags(st: dict) -> str:
    """Inverse of _parse_source_tags."""
    return "; ".join(f"{src}:{' '.join(tags)}" for src, tags in st.items())


# ── API helpers ────────────────────────────────────────────────────────────────

def api_get(path: str, params: dict | None = None) -> dict:
    return httpx.get(f"{BASE_URL}{path}", params=params, timeout=30).raise_for_status().json()  # type: ignore[union-attr]

def api_post(path: str, json_body: dict | None = None) -> dict:
    return httpx.post(f"{BASE_URL}{path}", json=json_body, timeout=60).raise_for_status().json()  # type: ignore[union-attr]

def api_delete(path: str) -> dict | None:
    r = httpx.delete(f"{BASE_URL}{path}", timeout=10)
    r.raise_for_status()
    try:
        return r.json()
    except Exception:
        return None


# ── Messages ───────────────────────────────────────────────────────────────────

class StreamCompleted(Message):
    """Fired by JobsTab when a stream download finishes."""

class RunPreset(Message):
    """Fired by PresetsTab to submit a preset job."""
    def __init__(self, preset: dict) -> None:
        super().__init__()
        self.preset = preset


# ── Progress bar ───────────────────────────────────────────────────────────────

def _fmt_progress(status: str, done: int, total: int, failed: int,
                   spinner: str = " ") -> str:
    """
    Always returns markup with consistent visual width so DataTable column
    width stays stable across update_cell calls.
    Layout: spinner(2) + bar(_BAR_WIDTH) + count/label(~12)
    """
    spin  = f"{spinner} "
    empty = "░" * _BAR_WIDTH

    if status == "pending":
        return f"[dim]{spin}{empty} pending…[/dim]"
    if status == "cancelled":
        return f"[dim]{spin}{empty} cancelled[/dim]"
    if total == 0:
        return f"{spin}[dim]{empty}[/dim] ?/?"
    filled = round(done / total * _BAR_WIDTH)
    bar    = f"[green]{'█' * filled}[/green][dim]{'░' * (_BAR_WIDTH - filled)}[/dim]"
    fail   = f" [red]{failed}✗[/red]" if failed else ""
    return f"{spin}{bar} [green]{done}[/green][dim]/{total}[/dim]{fail}"


# ── Image Preview ──────────────────────────────────────────────────────────────

class ImagePreview(Static):
    DEFAULT_CSS = f"""
    ImagePreview {{
        width: {_PREVIEW_WIDTH};
        height: 1fr;
        border-left: solid $panel-lighten-1;
        content-align: center middle;
        padding: 0;
        overflow: hidden;
    }}
    """

    def load(self, post_id: int, filetype: str) -> None:
        if filetype.lower() not in _IMAGE_TYPES:
            self.update(f"\n[dim]no preview\n({filetype})[/dim]")
            return
        self.update("[dim]loading…[/dim]")
        self._fetch(post_id)

    @work(thread=True)
    def _fetch(self, post_id: int) -> None:
        try:
            resp = httpx.get(f"{BASE_URL}/api/v1/library/{post_id}/file", timeout=20)
            resp.raise_for_status()
            rendered = self._halfblocks(resp.content)
        except Exception as e:
            self.app.call_from_thread(self.update, f"[red]{e}[/red]")
            return
        self.app.call_from_thread(self.update, rendered)

    def _halfblocks(self, data: bytes):
        from PIL import Image as PILImage
        from rich.text import Text

        panel_w = self.size.width or _PREVIEW_WIDTH
        panel_h = self.size.height or 24

        with PILImage.open(io.BytesIO(data)) as img:
            if hasattr(img, "n_frames") and img.n_frames > 1:
                img.seek(0)
            img = img.convert("RGB")
            iw, ih = img.size

            px_w = panel_w
            px_h = panel_h * 2
            if px_w * ih > px_h * iw:
                px_w = px_h * iw // ih
            else:
                px_h = px_w * ih // iw
                px_h += px_h % 2

            img = img.resize((px_w, px_h), PILImage.LANCZOS)
            W, H = img.size
            pixels = list(img.getdata())

        text = Text()
        for row in range(0, H - 1, 2):
            for col in range(W):
                r1, g1, b1 = pixels[row * W + col]
                r2, g2, b2 = pixels[(row + 1) * W + col]
                text.append("▄", style=f"rgb({r2},{g2},{b2}) on rgb({r1},{g1},{b1})")
            text.append("\n")
        return text


# ── Library Tab ────────────────────────────────────────────────────────────────

class LibraryTab(Widget):
    BINDINGS = [
        Binding("r",     "refresh",        "Refresh"),
        Binding("space", "toggle_preview", "Preview"),
        Binding("d",     "download",       "Download"),
        Binding("x",     "delete_post",    "Delete"),
    ]

    _posts:        list[dict]
    _sort:         str
    _animated:     str   # "all" | "animated" | "static"
    _ratings:      set   # subset of {"safe","questionable","explicit"}; empty = all
    _preview_open: bool

    _ALL_RATINGS = ("safe", "questionable", "explicit")

    def compose(self) -> ComposeResult:
        with Vertical():
            with Horizontal(id="search-bar"):
                yield Input(placeholder="author (artist tag)", id="author-input")
                yield Input(placeholder="tags  e.g. fox",      id="tag-input")
                yield Input(placeholder="limit", id="limit-input", value="20")
                yield Button("Search", variant="primary", id="search-btn")
            with Horizontal(id="sort-bar"):
                yield Label("Sort:",     id="sort-label")
                yield Button("Newest",   variant="primary",  id="sort-newest")
                yield Button("Oldest",   variant="default",  id="sort-oldest")
                yield Button("Score",    variant="default",  id="sort-score")
                yield Label("  Anim:",   id="anim-label")
                yield Button("All",      variant="primary",  id="anim-all")
                yield Button("Animated", variant="default",  id="anim-yes")
                yield Button("Static",   variant="default",  id="anim-no")
                yield Label("  Rating:", id="rating-label")
                yield Button("S",  variant="default", id="rating-safe")
                yield Button("Q",  variant="default", id="rating-questionable")
                yield Button("E",  variant="default", id="rating-explicit")
            with Horizontal(id="lib-content"):
                with Vertical(id="lib-left"):
                    yield DataTable(id="library-table", cursor_type="row")
                    yield Label("", id="lib-status")
                yield ImagePreview(id="image-preview")

    def on_mount(self) -> None:
        self._posts        = []
        self._sort         = "newest"
        self._animated     = "all"
        self._ratings      = set()
        self._preview_open = False
        self.query_one("#image-preview").display = False
        t = self.query_one("#library-table", DataTable)
        t.add_column("ID",      key="id")
        t.add_column("Source",  key="source")
        t.add_column("Post ID", key="post_id")
        t.add_column("Type",    key="type")
        t.add_column("Rating",  key="rating")
        t.add_column("Size",    key="size")
        t.add_column("Score",   key="score")
        t.add_column("Tags",    key="tags")
        self.load_posts()

    # ── sort ───────────────────────────────────────────────────────────────────

    @on(Button.Pressed, "#sort-newest")
    def _sort_newest(self) -> None: self._set_sort("newest")
    @on(Button.Pressed, "#sort-oldest")
    def _sort_oldest(self) -> None: self._set_sort("oldest")
    @on(Button.Pressed, "#sort-score")
    def _sort_score(self)  -> None: self._set_sort("score")

    def _set_sort(self, sort: str) -> None:
        self._sort = sort
        for sid, lbl in [("sort-newest","newest"),("sort-oldest","oldest"),("sort-score","score")]:
            self.query_one(f"#{sid}", Button).variant = "primary" if lbl == sort else "default"
        self.action_refresh()

    # ── animated filter ────────────────────────────────────────────────────────

    @on(Button.Pressed, "#anim-all")
    def _anim_all(self) -> None: self._set_animated("all")
    @on(Button.Pressed, "#anim-yes")
    def _anim_yes(self) -> None: self._set_animated("animated")
    @on(Button.Pressed, "#anim-no")
    def _anim_no(self)  -> None: self._set_animated("static")

    def _set_animated(self, v: str) -> None:
        self._animated = v
        for sid, lbl in [("anim-all","all"),("anim-yes","animated"),("anim-no","static")]:
            self.query_one(f"#{sid}", Button).variant = "primary" if lbl == v else "default"
        self.action_refresh()

    # ── rating filter (toggle) ─────────────────────────────────────────────────

    @on(Button.Pressed, "#rating-safe")
    def _rating_safe(self)         -> None: self._toggle_rating("safe")
    @on(Button.Pressed, "#rating-questionable")
    def _rating_questionable(self) -> None: self._toggle_rating("questionable")
    @on(Button.Pressed, "#rating-explicit")
    def _rating_explicit(self)     -> None: self._toggle_rating("explicit")

    def _toggle_rating(self, r: str) -> None:
        if r in self._ratings:
            self._ratings.discard(r)
        else:
            self._ratings.add(r)
        colors = {"safe": "green", "questionable": "yellow", "explicit": "red"}
        for rid in ("safe", "questionable", "explicit"):
            btn = self.query_one(f"#rating-{rid}", Button)
            btn.variant = "primary" if rid in self._ratings else "default"
        self.action_refresh()

    # ── data ───────────────────────────────────────────────────────────────────

    @work(thread=True)
    def load_posts(self, author: str = "", tags: str = "", limit: int = 20) -> None:
        params: dict = {"limit": limit, "sort": self._sort}
        if author.strip():
            params["author"] = author.strip()
        if tags.strip():
            params["tags"] = "+".join(tags.strip().split())
        if self._animated == "animated":
            params["animated"] = "true"
        elif self._animated == "static":
            params["animated"] = "false"
        if self._ratings:
            params["ratings"] = ",".join(self._ratings)
        try:
            posts = (api_get("/api/v1/library", params) or {}).get("data") or []
        except Exception as e:
            self.app.call_from_thread(
                self.query_one("#lib-status", Label).update, f"[red]{e}[/red]"
            )
            return
        self.app.call_from_thread(self._populate, posts)

    _RATING_COLOR = {"safe": "green", "questionable": "yellow", "explicit": "red"}

    def _populate(self, posts: list[dict]) -> None:
        self._posts = posts
        t = self.query_one("#library-table", DataTable)
        t.clear()
        for p in posts:
            size_kb = f"{(p.get('Size') or 0) // 1024} KB"
            tags    = p.get("Tags") or []
            preview = ", ".join(tags[:4]) + ("…" if len(tags) > 4 else "")
            rating  = p.get("Rating") or ""
            color   = self._RATING_COLOR.get(rating, "white")
            t.add_row(
                str(p.get("ID", "")),
                p.get("Source", ""),
                p.get("PostID", ""),
                p.get("Filetype", ""),
                f"[{color}]{rating}[/{color}]" if rating else "",
                size_kb,
                str(p.get("Score", 0)),
                preview,
            )
        self.query_one("#lib-status", Label).update(
            f"{len(posts)} posts  [dim]space=preview  d=download  x=delete  r=refresh[/dim]"
        )
        if self._preview_open:
            self._load_preview_for_cursor()

    @on(Button.Pressed, "#search-btn")
    @on(Input.Submitted, "#author-input, #tag-input, #limit-input")
    def on_search(self) -> None:
        self.action_refresh()

    def action_refresh(self) -> None:
        author = self.query_one("#author-input", Input).value
        tags   = self.query_one("#tag-input",    Input).value
        try:
            limit = int(self.query_one("#limit-input", Input).value or "20")
        except ValueError:
            limit = 20
        self.load_posts(author, tags, limit)

    def on_stream_completed(self, _: StreamCompleted) -> None:
        self.action_refresh()

    # ── preview ────────────────────────────────────────────────────────────────

    def on_resize(self) -> None:
        if self._preview_open and self.size.width < _PREVIEW_MIN_TERMINAL_WIDTH:
            self._close_preview()

    def on_data_table_row_highlighted(self, _: DataTable.RowHighlighted) -> None:
        if self._preview_open:
            self._load_preview_for_cursor()

    def action_toggle_preview(self) -> None:
        if self._preview_open:
            self._close_preview()
        elif self.size.width >= _PREVIEW_MIN_TERMINAL_WIDTH:
            self._open_preview()

    def _open_preview(self) -> None:
        self._preview_open = True
        self.query_one("#image-preview").display = True
        self._load_preview_for_cursor()

    def _close_preview(self) -> None:
        self._preview_open = False
        self.query_one("#image-preview").display = False

    def _load_preview_for_cursor(self) -> None:
        t   = self.query_one("#library-table", DataTable)
        idx = t.cursor_row
        if idx < 0 or idx >= len(self._posts):
            return
        p = self._posts[idx]
        self.query_one("#image-preview", ImagePreview).load(
            p.get("ID", 0), p.get("Filetype", "")
        )

    # ── download ───────────────────────────────────────────────────────────────

    def action_download(self) -> None:
        t   = self.query_one("#library-table", DataTable)
        idx = t.cursor_row
        if idx < 0 or idx >= len(self._posts):
            return
        self._do_download(self._posts[idx])

    def action_delete_post(self) -> None:
        t   = self.query_one("#library-table", DataTable)
        idx = t.cursor_row
        if idx < 0 or idx >= len(self._posts):
            return
        p = self._posts[idx]
        self.app.push_screen(DeleteConfirmModal(p), self._on_delete_confirmed)

    def _on_delete_confirmed(self, post: dict | None) -> None:
        if post is not None:
            self._do_delete(post)

    @work(thread=True)
    def _do_delete(self, p: dict) -> None:
        post_id = p.get("ID", 0)
        try:
            api_delete(f"/api/v1/library/{post_id}")
            self.app.call_from_thread(self.action_refresh)
            self.app.call_from_thread(
                self.query_one("#lib-status", Label).update,
                f"[green]Deleted post {post_id}[/green]"
            )
        except Exception as e:
            self.app.call_from_thread(
                self.query_one("#lib-status", Label).update,
                f"[red]Delete failed: {e}[/red]"
            )

    @work(thread=True)
    def _do_download(self, p: dict) -> None:
        post_id  = p.get("ID", 0)
        source   = p.get("Source", "unknown")
        filetype = p.get("Filetype", "bin")
        dest     = DOWNLOADS_DIR / f"{source}_{post_id}.{filetype}"
        DOWNLOADS_DIR.mkdir(parents=True, exist_ok=True)
        try:
            resp = httpx.get(f"{BASE_URL}/api/v1/library/{post_id}/file", timeout=60)
            resp.raise_for_status()
            dest.write_bytes(resp.content)
            self.app.call_from_thread(
                self.query_one("#lib-status", Label).update,
                f"[green]Saved → {dest}[/green]"
            )
        except Exception as e:
            self.app.call_from_thread(
                self.query_one("#lib-status", Label).update,
                f"[red]Download failed: {e}[/red]"
            )


# ── Delete Confirmation Modal ──────────────────────────────────────────────────

class DeleteConfirmModal(ModalScreen[dict | None]):
    DEFAULT_CSS = "DeleteConfirmModal { align: center middle; }"
    BINDINGS = [
        Binding("x",      "confirm", "Confirm"),
        Binding("escape", "cancel",  "Cancel"),
    ]

    def __init__(self, post: dict) -> None:
        super().__init__()
        self._post = post

    def compose(self) -> ComposeResult:
        post_id  = self._post.get("PostID", "")
        source   = self._post.get("Source", "")
        ftype    = self._post.get("Filetype", "")
        with Container(id="confirm-container"):
            yield Static(
                f"[bold red]Delete post?[/bold red]\n\n"
                f"[white]{source}[/white]  [dim]{post_id}.{ftype}[/dim]\n\n"
                f"File will be removed from disk.\n"
                f"It will [bold]not[/bold] be re-downloaded.\n\n"
                f"[bold]x[/bold] = confirm   [bold]Esc[/bold] = cancel",
                id="confirm-text",
            )

    def action_confirm(self) -> None:
        self.dismiss(self._post)

    def action_cancel(self) -> None:
        self.dismiss(None)


# ── Deleted Tab ────────────────────────────────────────────────────────────────

class DeletedTab(Widget):
    BINDINGS = [
        Binding("r", "refresh", "Refresh"),
        Binding("c", "clear",   "Clear all"),
    ]

    _posts: list[dict]

    def compose(self) -> ComposeResult:
        with Vertical():
            with Horizontal(id="deleted-bar"):
                yield Button("Refresh [r]", id="del-refresh-btn")
                yield Button("Clear all [c]", variant="error", id="del-clear-btn")
                yield Label("", id="deleted-status")
            yield DataTable(id="deleted-table", cursor_type="row")

    def on_mount(self) -> None:
        self._posts = []
        t = self.query_one("#deleted-table", DataTable)
        t.add_column("ID",         key="id",         width=6)
        t.add_column("Source",     key="source",     width=10)
        t.add_column("Post ID",    key="post_id",    width=30)
        t.add_column("Type",       key="type",       width=6)
        t.add_column("Rating",     key="rating",     width=12)
        t.add_column("Size",       key="size",       width=8)
        t.add_column("Deleted at", key="deleted_at", width=19)
        self.action_refresh()

    @on(Button.Pressed, "#del-refresh-btn")
    def _on_refresh(self) -> None: self.action_refresh()

    @on(Button.Pressed, "#del-clear-btn")
    def _on_clear(self) -> None: self.action_clear()

    def action_refresh(self) -> None:
        self._load()

    @work(thread=True)
    def _load(self) -> None:
        try:
            posts = (api_get("/api/v1/library/deleted", {"limit": "500"}) or {}).get("data") or []
        except Exception as e:
            self.app.call_from_thread(
                self.query_one("#deleted-status", Label).update, f"[red]{e}[/red]"
            )
            return
        self.app.call_from_thread(self._populate, posts)

    _RATING_COLOR = {"safe": "green", "questionable": "yellow", "explicit": "red"}

    def _populate(self, posts: list[dict]) -> None:
        self._posts = posts
        t = self.query_one("#deleted-table", DataTable)
        t.clear()
        for p in posts:
            rating  = p.get("Rating") or ""
            color   = self._RATING_COLOR.get(rating, "white")
            deleted = (p.get("DeletedAt") or {}).get("Time", "")[:19].replace("T", " ")
            t.add_row(
                str(p.get("ID", "")),
                p.get("Source", ""),
                p.get("PostID", ""),
                p.get("Filetype", ""),
                f"[{color}]{rating}[/{color}]" if rating else "",
                f"{(p.get('Size') or 0) // 1024} KB",
                deleted,
            )
        self.query_one("#deleted-status", Label).update(
            f"{len(posts)} deleted  [dim]c=clear all  r=refresh[/dim]"
        )

    def action_clear(self) -> None:
        self._do_clear()

    @work(thread=True)
    def _do_clear(self) -> None:
        try:
            result = api_delete("/api/v1/library/deleted") or {}
            n = ((result or {}).get("data") or {}).get("cleared", "?")
            self.app.call_from_thread(self._populate, [])
            self.app.call_from_thread(
                self.query_one("#deleted-status", Label).update,
                f"[green]Cleared {n} records[/green]"
            )
        except Exception as e:
            self.app.call_from_thread(
                self.query_one("#deleted-status", Label).update,
                f"[red]{e}[/red]"
            )


# ── Jobs Tab ───────────────────────────────────────────────────────────────────

class NewJobModal(ModalScreen[dict | None]):
    BINDINGS = [Binding("escape", "cancel", "Cancel")]

    def compose(self) -> ComposeResult:
        with Container(id="modal-container"):
            yield Static("[b]New Download Job[/b]", id="modal-title")
            yield Input(placeholder="author (artist tag, e.g. kenket)", id="job-author")
            yield Input(placeholder="tags (e.g. fox rating:safe)", id="job-tags")
            yield Input(placeholder="limit per source (default 20)", id="job-limit", value="20")
            yield Input(placeholder="sources (comma sep., empty = all)", id="job-sources")
            yield Input(placeholder='per-source tags  e.g. e621:order:score -animated', id="job-source-tags")
            with Horizontal(id="modal-buttons"):
                yield Button("Create", variant="primary", id="create-btn")
                yield Button("Cancel", variant="default", id="cancel-btn")

    @on(Button.Pressed, "#create-btn")
    def do_create(self) -> None:
        author   = self.query_one("#job-author", Input).value.strip()
        tags_raw = self.query_one("#job-tags",   Input).value.strip()
        tags     = tags_raw.split() if tags_raw else []
        try:
            limit = int(self.query_one("#job-limit", Input).value or "20")
        except ValueError:
            limit = 20
        sources_raw = self.query_one("#job-sources", Input).value.strip()
        sources     = [s.strip() for s in sources_raw.split(",") if s.strip()]
        payload: dict = {"tags": tags, "limit": limit}
        if author:
            payload["author"] = author
        if sources:
            payload["sources"] = sources
        st = _parse_source_tags(self.query_one("#job-source-tags", Input).value)
        if st:
            payload["source_tags"] = st
        self.dismiss(payload)

    @on(Button.Pressed, "#cancel-btn")
    def action_cancel(self) -> None:
        self.dismiss(None)


STATUS_COLOR = {
    "done":      "green",
    "running":   "yellow",
    "cancelled": "dim",
    "failed":    "red",
    "pending":   "blue",
    "streaming": "cyan",
}


class JobsTab(Widget):
    BINDINGS = [
        Binding("n", "new_job",    "New Job"),
        Binding("r", "refresh",    "Refresh"),
        Binding("x", "cancel_job", "Cancel"),
    ]

    def compose(self) -> ComposeResult:
        with Vertical():
            yield DataTable(id="jobs-table", cursor_type="row")
            yield Label("", id="jobs-status")

    def on_mount(self) -> None:
        self._streams: dict[str, dict] = {}
        t = self.query_one("#jobs-table", DataTable)
        t.add_column("ID",       key="id",       width=10)
        t.add_column("Status",   key="status",   width=11)
        t.add_column("Tags",     key="tags",     width=30)
        t.add_column("Created",  key="created",  width=19)
        t.add_column("Progress", key="progress", width=_BAR_WIDTH + 16)
        self.set_interval(0.1, self._tick)
        self.load_jobs()

    # ── spinner tick ───────────────────────────────────────────────────────────

    def _tick(self) -> None:
        t = self.query_one("#jobs-table", DataTable)
        for rk, s in list(self._streams.items()):
            if not s["active"]:
                continue
            s["spin_idx"] = (s["spin_idx"] + 1) % len(_SPINNER)
            try:
                t.update_cell(rk, "progress",
                    _fmt_progress("streaming", s["done"], s["total"], s["failed"],
                                  spinner=_SPINNER[s["spin_idx"]]))
            except Exception:
                pass

    # ── data ───────────────────────────────────────────────────────────────────

    @work(thread=True)
    def load_jobs(self) -> None:
        try:
            jobs = (api_get("/api/v1/jobs") or {}).get("data") or []
        except Exception as e:
            self.app.call_from_thread(
                self.query_one("#jobs-status", Label).update, f"[red]{e}[/red]"
            )
            return
        self.app.call_from_thread(self._populate, jobs)

    def _populate(self, jobs: list[dict]) -> None:
        t    = self.query_one("#jobs-table", DataTable)
        live = {k for k, s in self._streams.items() if s["active"]}
        t.clear()
        for j in jobs:
            self._add_row(t, j)
        for rk in live:
            self._add_stub(t, rk, self._streams[rk])
        self.query_one("#jobs-status", Label).update(
            f"{len(jobs) + len(live)} jobs  [dim]n=new  x=cancel  r=refresh[/dim]"
        )

    def _add_row(self, t: DataTable, j: dict) -> None:
        status  = j.get("status", "")
        color   = STATUS_COLOR.get(status, "white")
        created = (j.get("created_at") or "")[:19].replace("T", " ")
        done, total, failed = j.get("done", 0), j.get("total", 0), j.get("failed", 0)
        t.add_row(
            j["id"][:8],
            f"[{color}]{status}[/{color}]",
            " ".join(j.get("tags") or [])[:28],
            created,
            _fmt_progress(status, done, total, failed),
            key=j["id"],
        )

    def _add_stub(self, t: DataTable, rk: str, s: dict) -> None:
        t.add_row(
            rk[-8:],
            "[cyan]streaming[/cyan]",
            s["tags_str"],
            s["created"],
            _fmt_progress("streaming", s["done"], s["total"], s["failed"],
                          spinner=_SPINNER[s["spin_idx"]]),
            key=rk,
        )

    # ── actions ────────────────────────────────────────────────────────────────

    def action_new_job(self) -> None:
        def handle(payload: dict | None) -> None:
            if payload:
                self._submit(payload)
        self.app.push_screen(NewJobModal(), handle)

    def submit_preset(self, preset: dict) -> None:
        payload: dict = {"tags": preset.get("tags", []), "limit": preset.get("limit", 20)}
        if preset.get("author"):
            payload["author"] = preset["author"]
        if sources := preset.get("sources"):
            payload["sources"] = sources
        if st := preset.get("source_tags"):
            payload["source_tags"] = st
        self._submit(payload)

    @work(thread=True)
    def _submit(self, payload: dict) -> None:
        import uuid as _uuid
        author  = payload.get("author", "")
        tags    = payload.get("tags", [])
        sources = payload.get("sources", [])
        label   = author if author else " ".join(tags)
        rk = f"stream-{_uuid.uuid4().hex[:8]}"
        s: dict = {
            "done": 0, "total": 0, "failed": 0,
            "spin_idx": 0, "active": True, "cancelled": False, "resp": None,
            "tags_str": label[:28],
            "created":  datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S"),
        }
        self._streams[rk] = s
        self.app.call_from_thread(self._insert_stub, rk, s)
        self._stream(tags, payload.get("limit", 20), sources, author,
                     payload.get("source_tags", {}), rk)

    def _insert_stub(self, rk: str, s: dict) -> None:
        self._add_stub(self.query_one("#jobs-table", DataTable), rk, s)

    def action_cancel_job(self) -> None:
        t = self.query_one("#jobs-table", DataTable)
        if t.cursor_row < 0 or not t.row_count:
            return
        rk, _ = t.coordinate_to_cell_key((t.cursor_row, 0))
        job_id = str(rk.value)
        if job_id.startswith("stream-"):
            s = self._streams.get(job_id)
            if s and s["active"]:
                s["cancelled"] = True
                resp = s.get("resp")
                if resp is not None:
                    try:
                        resp.close()
                    except Exception:
                        pass
        else:
            self._cancel(job_id)

    @work(thread=True)
    def _cancel(self, job_id: str) -> None:
        try:
            api_delete(f"/api/v1/jobs/{job_id}")
        except Exception:
            pass
        self.app.call_from_thread(self.load_jobs)

    def action_refresh(self) -> None:
        self.load_jobs()

    @work(thread=True)
    def _stream(self, tags: list[str], limit: int, sources: list[str],
                author: str, source_tags: dict, rk: str) -> None:
        s = self._streams[rk]
        params: dict = {"tags": "+".join(tags), "limit": str(limit)}
        if author:
            params["author"] = author
        if sources:
            params["sources"] = ",".join(sources)
        if source_tags:
            params["source_tags"] = json.dumps(source_tags, separators=(",", ":"))

        def set_final(status: str) -> None:
            color = STATUS_COLOR.get(status, "white")
            t = self.query_one("#jobs-table", DataTable)
            try:
                t.update_cell(rk, "status",
                    f"[{color}]{status}[/{color}]")
                t.update_cell(rk, "progress",
                    _fmt_progress(status, s["done"], s["total"], s["failed"]))
            except Exception:
                pass

        try:
            with httpx.stream("GET", f"{BASE_URL}/api/v1/stream",
                              params=params, timeout=None) as resp:
                s["resp"] = resp
                if s["cancelled"]:
                    resp.close()
                else:
                    resp.raise_for_status()
                    for line in resp.iter_lines():
                        if s["cancelled"]:
                            break
                        if not line.startswith("data: "):
                            continue
                        try:
                            ev = json.loads(line[6:])
                        except json.JSONDecodeError:
                            continue
                        etype = ev.get("type")
                        if etype == "found":
                            s["total"] = ev.get("count", 0)
                        elif etype == "downloaded":
                            s["done"] += 1
                        elif etype == "failed":
                            s["failed"] += 1
                        elif etype == "done":
                            s["done"]   = ev.get("done",   s["done"])
                            s["failed"] = ev.get("failed", s["failed"])
                            s["active"] = False
                            final = "done" if not s["failed"] else "failed"
                            self.app.call_from_thread(set_final, final)
                            self.app.call_from_thread(self.post_message, StreamCompleted())
                            break
        except Exception as e:
            if not s["cancelled"]:
                self.app.call_from_thread(
                    self.query_one("#jobs-status", Label).update, f"[red]{e}[/red]"
                )
        finally:
            if s["cancelled"]:
                s["active"] = False
                self.app.call_from_thread(set_final, "cancelled")
            elif s["active"]:
                s["active"] = False


# ── Presets Tab ────────────────────────────────────────────────────────────────

class PresetsTab(Widget):
    BINDINGS = [
        Binding("n", "new_preset",    "New"),
        Binding("d", "delete_preset", "Delete"),
        Binding("r", "run_preset",    "Run"),
        Binding("a", "run_all",       "Run All"),
    ]

    _presets:  list[dict]
    _selected: int  # index into _presets, -1 = none / new

    def compose(self) -> ComposeResult:
        with Horizontal(id="presets-layout"):
            with Vertical(id="presets-list-panel"):
                yield DataTable(id="presets-table", cursor_type="row")
                with Horizontal(id="preset-list-btns"):
                    yield Button("New  [n]",      id="new-preset-btn")
                    yield Button("Delete [d]",    variant="error",   id="del-preset-btn")
                    yield Button("▶ Run  [r]",    variant="success", id="run-preset-btn")
                    yield Button("▶▶ Run All [a]", variant="success", id="run-all-btn")
            with Vertical(id="preset-form-panel"):
                yield Static("[b]Preset details[/b]", id="form-title")
                yield Label("Name",                              classes="form-label")
                yield Input(placeholder="My fox collection",     id="preset-name")
                yield Label("Author (artist tag)",               classes="form-label")
                yield Input(placeholder="kenket",                id="preset-author")
                yield Label("Tags (space separated)",            classes="form-label")
                yield Input(placeholder="fox rating:safe",       id="preset-tags")
                yield Label("Sources (comma sep., empty = all)", classes="form-label")
                yield Input(placeholder="e621, gelbooru",        id="preset-sources")
                yield Label("Per-source tags  e.g. e621:order:score -animated", classes="form-label")
                yield Input(placeholder="e621:order:score; gelbooru:sort:score", id="preset-source-tags")
                yield Label("Limit per source",                  classes="form-label")
                yield Input(placeholder="20", value="20",        id="preset-limit")
                yield Button("Save", variant="primary",          id="save-preset-btn")
                yield Label("", id="preset-status")

    def on_mount(self) -> None:
        self._presets  = []
        self._selected = -1
        t = self.query_one("#presets-table", DataTable)
        t.add_column("Name",    key="name",    width=22)
        t.add_column("Tags",    key="tags",    width=32)
        t.add_column("Sources", key="sources", width=16)
        t.add_column("Lim/src", key="limit",   width=7)
        self._load_presets()

    # ── persistence ────────────────────────────────────────────────────────────

    def _load_presets(self) -> None:
        if PRESETS_FILE.exists():
            try:
                self._presets = json.loads(PRESETS_FILE.read_text())
            except Exception:
                self._presets = []
        self._populate()

    def _persist(self) -> None:
        PRESETS_FILE.write_text(json.dumps(self._presets, indent=2, ensure_ascii=False))

    # ── table ──────────────────────────────────────────────────────────────────

    def _populate(self) -> None:
        t = self.query_one("#presets-table", DataTable)
        t.clear()
        for p in self._presets:
            t.add_row(
                p.get("name", ""),
                " ".join(p.get("tags", [])),
                ", ".join(p.get("sources", [])) or "all",
                str(p.get("limit", 20)),
            )

    def on_data_table_row_highlighted(self, ev: DataTable.RowHighlighted) -> None:
        idx = ev.cursor_row
        if 0 <= idx < len(self._presets):
            self._selected = idx
            self._fill_form(self._presets[idx])

    def _fill_form(self, p: dict) -> None:
        self.query_one("#preset-name",        Input).value = p.get("name", "")
        self.query_one("#preset-author",      Input).value = p.get("author", "")
        self.query_one("#preset-tags",        Input).value = " ".join(p.get("tags", []))
        self.query_one("#preset-sources",     Input).value = ", ".join(p.get("sources", []))
        self.query_one("#preset-source-tags", Input).value = _fmt_source_tags(p.get("source_tags", {}))
        self.query_one("#preset-limit",       Input).value = str(p.get("limit", 20))
        self.query_one("#preset-status",      Label).update("")

    def _form_to_dict(self) -> dict:
        name    = self.query_one("#preset-name",        Input).value.strip()
        author  = self.query_one("#preset-author",      Input).value.strip()
        tags    = self.query_one("#preset-tags",        Input).value.strip().split()
        raw_src = self.query_one("#preset-sources",     Input).value.strip()
        sources = [s.strip() for s in raw_src.split(",") if s.strip()]
        st      = _parse_source_tags(self.query_one("#preset-source-tags", Input).value)
        try:
            limit = int(self.query_one("#preset-limit", Input).value or "20")
        except ValueError:
            limit = 20
        d: dict = {"name": name, "tags": tags, "sources": sources, "limit": limit}
        if author:
            d["author"] = author
        if st:
            d["source_tags"] = st
        return d

    # ── actions ────────────────────────────────────────────────────────────────

    @on(Button.Pressed, "#save-preset-btn")
    def action_save(self) -> None:
        d = self._form_to_dict()
        if not d["name"]:
            self.query_one("#preset-status", Label).update("[red]Name is required[/red]")
            return
        if 0 <= self._selected < len(self._presets):
            self._presets[self._selected] = d
        else:
            self._presets.append(d)
            self._selected = len(self._presets) - 1
        self._persist()
        self._populate()
        self.query_one("#preset-status", Label).update("[green]Saved ✓[/green]")

    def action_new_preset(self) -> None:
        self._selected = -1
        for fid in ("#preset-name", "#preset-author", "#preset-tags",
                    "#preset-sources", "#preset-source-tags"):
            self.query_one(fid, Input).value = ""
        self.query_one("#preset-limit",  Input).value = "20"
        self.query_one("#preset-status", Label).update("")
        self.query_one("#preset-name",   Input).focus()

    @on(Button.Pressed, "#new-preset-btn")
    def _on_new(self) -> None: self.action_new_preset()

    def action_delete_preset(self) -> None:
        if not (0 <= self._selected < len(self._presets)):
            return
        self._presets.pop(self._selected)
        self._selected = min(self._selected, len(self._presets) - 1)
        self._persist()
        self._populate()
        self.query_one("#preset-status", Label).update("[dim]Deleted[/dim]")

    @on(Button.Pressed, "#del-preset-btn")
    def _on_del(self) -> None: self.action_delete_preset()

    def action_run_preset(self) -> None:
        if not (0 <= self._selected < len(self._presets)):
            self.query_one("#preset-status", Label).update("[red]Select a preset first[/red]")
            return
        preset = self._presets[self._selected]
        self.post_message(RunPreset(preset))
        self.query_one("#preset-status", Label).update(
            f"[cyan]Submitted: {preset.get('name', '')}[/cyan]"
        )

    @on(Button.Pressed, "#run-preset-btn")
    def _on_run(self) -> None: self.action_run_preset()

    def action_run_all(self) -> None:
        if not self._presets:
            self.query_one("#preset-status", Label).update("[red]No presets to run[/red]")
            return
        for preset in self._presets:
            self.post_message(RunPreset(preset))
        self.query_one("#preset-status", Label).update(
            f"[cyan]Submitted {len(self._presets)} preset(s)[/cyan]"
        )

    @on(Button.Pressed, "#run-all-btn")
    def _on_run_all(self) -> None: self.action_run_all()


# ── Health Tab ─────────────────────────────────────────────────────────────────

class HealthTab(Widget):
    BINDINGS = [
        Binding("r", "check", "Check"),
        Binding("h", "heal",  "Heal"),
    ]

    healing: reactive[bool] = reactive(False)

    def compose(self) -> ComposeResult:
        with Vertical(id="health-inner"):
            yield Static("[b]Library Health[/b]\n", id="health-title")
            yield Static("Loading…", id="health-report")
            with Horizontal(id="health-buttons"):
                yield Button("Check  [r]",        id="check-btn")
                yield Button("Heal missing  [h]", variant="warning", id="heal-btn")
            yield Static("", id="heal-report")

    def on_mount(self) -> None:
        self.action_check()

    @on(Button.Pressed, "#check-btn")
    def action_check(self) -> None: self._do_check()

    @on(Button.Pressed, "#heal-btn")
    def action_heal(self) -> None:
        if not self.healing:
            self._do_heal()

    @work(thread=True)
    def _do_check(self) -> None:
        try:
            report = (api_get("/api/v1/library/health") or {}).get("data", {})
        except Exception as e:
            self.app.call_from_thread(
                self.query_one("#health-report", Static).update, f"[red]{e}[/red]"
            )
            return
        self.app.call_from_thread(self._show_health, report)

    def _show_health(self, r: dict) -> None:
        total, healthy = r.get("total", 0), r.get("healthy", 0)
        missing, corrupted = r.get("missing", 0), r.get("corrupted", 0)
        mc = "green" if missing   == 0 else "red"
        cc = "green" if corrupted == 0 else "yellow"
        txt = (f"[b]Total:[/b]     {total}\n"
               f"[b]Healthy:[/b]   [green]{healthy}[/green]\n"
               f"[b]Missing:[/b]   [{mc}]{missing}[/{mc}]\n"
               f"[b]Corrupted:[/b] [{cc}]{corrupted}[/{cc}]")
        if ids := r.get("missing_ids"):
            txt += f"\n[dim]Missing IDs:   {ids}[/dim]"
        if ids := r.get("corrupted_ids"):
            txt += f"\n[dim]Corrupted IDs: {ids}[/dim]"
        self.query_one("#health-report", Static).update(txt)
        self.query_one("#heal-report",   Static).update("")

    @work(thread=True)
    def _do_heal(self) -> None:
        self.app.call_from_thread(self._set_healing, True)
        try:
            report = (api_post("/api/v1/library/heal") or {}).get("data", {})
            self.app.call_from_thread(self._show_heal, report)
        except Exception as e:
            self.app.call_from_thread(
                self.query_one("#heal-report", Static).update, f"[red]{e}[/red]"
            )
        finally:
            self.app.call_from_thread(self._set_healing, False)
        self._do_check()

    def _set_healing(self, v: bool) -> None:
        self.healing    = v
        btn             = self.query_one("#heal-btn", Button)
        btn.label       = "Healing…" if v else "Heal missing  [h]"
        btn.disabled    = v

    def _show_heal(self, r: dict) -> None:
        missing, corrupted = r.get("missing", 0), r.get("corrupted", 0)
        healed,  failed    = r.get("healed",  0), r.get("failed",   0)
        fc = "red" if failed else "green"
        self.query_one("#heal-report", Static).update(
            f"\n[b]Heal:[/b]  missing={missing}  corrupted={corrupted}"
            f"  [green]healed={healed}[/green]  [{fc}]failed={failed}[/{fc}]"
        )


# ── CSS ────────────────────────────────────────────────────────────────────────

CSS = """
/* Search + sort bars */
#search-bar { height: 3; padding: 0 1; }
#search-bar Input  { width: 1fr; margin-right: 1; }
#search-bar Button { width: 12; }

#sort-bar { height: 3; padding: 0 1; align: left middle; }
#sort-label, #anim-label, #rating-label { margin-right: 1; color: $text-muted; }
#sort-bar Button { width: 10; margin-right: 1; }
#anim-all, #anim-yes, #anim-no { width: 10; margin-right: 1; }
#rating-safe, #rating-questionable, #rating-explicit { width: 3; margin-right: 1; }

/* Library */
#lib-content  { height: 1fr; }
#lib-left     { width: 1fr; height: 1fr; }
#library-table { height: 1fr; }
#lib-status, #jobs-status { height: 1; padding: 0 1; color: $text-muted; }

/* Jobs */
#jobs-table { height: 1fr; }

/* Presets */
#presets-layout { height: 1fr; }
#presets-list-panel {
    width: 1fr;
    height: 1fr;
    border-right: solid $panel-lighten-1;
}
#presets-table { height: 1fr; }
#preset-list-btns { height: 3; padding: 0 1; align: left middle; }
#preset-list-btns Button { margin-right: 1; }

#preset-form-panel { width: 52; height: 1fr; padding: 1 2; }
.form-label { color: $text-muted; margin-top: 1; height: 1; }
#save-preset-btn { margin-top: 1; }
#preset-status { height: 1; margin-top: 1; }

/* Modal */
NewJobModal { align: center middle; }
#modal-container {
    width: 64; height: auto;
    border: round $primary;
    padding: 1 2;
    background: $surface;
}
#modal-title { text-align: center; margin-bottom: 1; }
#modal-container Input { margin-bottom: 1; }
#modal-buttons { height: 3; align: right middle; margin-top: 1; }
#modal-buttons Button { margin-left: 1; }

/* Health */
#health-inner   { padding: 2; }
#health-title   { margin-bottom: 1; }
#health-report  { margin-bottom: 2; }
#health-buttons { height: 3; }
#health-buttons Button { margin-right: 1; }
#heal-report    { margin-top: 1; }

/* Fill height */
TabPane { height: 1fr; }
LibraryTab, DeletedTab, JobsTab, PresetsTab, HealthTab { height: 1fr; }

/* Delete confirm modal */
#confirm-container {
    width: 60;
    height: auto;
    border: tall $error;
    background: $surface;
    padding: 2 4;
    align: center middle;
}
#confirm-text { text-align: center; }

/* Deleted tab */
#deleted-bar { height: 3; align: left middle; }
#deleted-bar Button { margin-right: 1; }
#deleted-status { margin-left: 2; }
"""


# ── App ────────────────────────────────────────────────────────────────────────

class FurLibApp(App):
    CSS   = CSS
    TITLE = "FurLib TUI"
    BINDINGS = [Binding("q", "quit", "Quit")]

    def compose(self) -> ComposeResult:
        yield Header()
        with TabbedContent(initial="tab-library"):
            with TabPane("Library", id="tab-library"):
                yield LibraryTab()
            with TabPane("Deleted", id="tab-deleted"):
                yield DeletedTab()
            with TabPane("Jobs",    id="tab-jobs"):
                yield JobsTab()
            with TabPane("Presets", id="tab-presets"):
                yield PresetsTab()
            with TabPane("Health",  id="tab-health"):
                yield HealthTab()
        yield Footer()

    def on_run_preset(self, msg: RunPreset) -> None:
        self.query_one(JobsTab).submit_preset(msg.preset)
        self.query_one(TabbedContent).active = "tab-jobs"


if __name__ == "__main__":
    FurLibApp().run()
