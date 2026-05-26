from __future__ import annotations

import io
import json
from datetime import datetime, timezone

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

BASE_URL = "http://localhost:8080"
_SPINNER = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
_IMAGE_TYPES = {"jpg", "jpeg", "png", "gif", "webp"}
_PREVIEW_MIN_TERMINAL_WIDTH = 120
_PREVIEW_WIDTH = 42


# ── API helpers ────────────────────────────────────────────────────────────────

def api_get(path: str, params: dict | None = None) -> dict:
    return httpx.get(f"{BASE_URL}{path}", params=params, timeout=30).raise_for_status().json()  # type: ignore[union-attr]

def api_post(path: str, json_body: dict | None = None) -> dict:
    return httpx.post(f"{BASE_URL}{path}", json=json_body, timeout=60).raise_for_status().json()  # type: ignore[union-attr]

def api_delete(path: str) -> None:
    httpx.delete(f"{BASE_URL}{path}", timeout=10).raise_for_status()


# ── Messages ───────────────────────────────────────────────────────────────────

class StreamCompleted(Message):
    """Fired by JobsTab when a stream download finishes."""


# ── Image Preview ──────────────────────────────────────────────────────────────

class ImagePreview(Static):
    """Right-side panel that renders a post image as half-block terminal art."""

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

            # fit inside panel, preserving aspect ratio
            # terminal chars ≈ 0.5 aspect; half-blocks: 1 char = 2px tall
            px_w = panel_w
            px_h = panel_h * 2
            if px_w * ih > px_h * iw:
                px_w = px_h * iw // ih
            else:
                px_h = px_w * ih // iw
                px_h += px_h % 2  # must be even

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
        Binding("r",      "refresh",        "Refresh"),
        Binding("space",  "toggle_preview", "Preview", show=True),
    ]

    _posts: list[dict]
    _sort: str
    _preview_open: bool

    def compose(self) -> ComposeResult:
        with Vertical():
            with Horizontal(id="search-bar"):
                yield Input(placeholder="tags  e.g. fox rating:safe", id="tag-input")
                yield Input(placeholder="limit", id="limit-input", value="20")
                yield Button("Search", variant="primary", id="search-btn")
            with Horizontal(id="sort-bar"):
                yield Label("Sort:", id="sort-label")
                yield Button("Newest", variant="primary", id="sort-newest")
                yield Button("Oldest", variant="default", id="sort-oldest")
                yield Button("Score",  variant="default", id="sort-score")
            with Horizontal(id="lib-content"):
                with Vertical(id="lib-left"):
                    yield DataTable(id="library-table", cursor_type="row")
                    yield Label("", id="lib-status")
                yield ImagePreview(id="image-preview")

    def on_mount(self) -> None:
        self._posts = []
        self._sort = "newest"
        self._preview_open = False
        self.query_one("#image-preview").display = False
        t = self.query_one("#library-table", DataTable)
        t.add_column("ID",      key="id")
        t.add_column("Source",  key="source")
        t.add_column("Post ID", key="post_id")
        t.add_column("Type",    key="type")
        t.add_column("Size",    key="size")
        t.add_column("Score",   key="score")
        t.add_column("Tags",    key="tags")
        self.load_posts()

    # ── sort buttons ───────────────────────────────────────────────────────────

    @on(Button.Pressed, "#sort-newest")
    def _sort_newest(self) -> None:
        self._set_sort("newest")

    @on(Button.Pressed, "#sort-oldest")
    def _sort_oldest(self) -> None:
        self._set_sort("oldest")

    @on(Button.Pressed, "#sort-score")
    def _sort_score(self) -> None:
        self._set_sort("score")

    def _set_sort(self, sort: str) -> None:
        self._sort = sort
        for sid, label in [("sort-newest", "newest"), ("sort-oldest", "oldest"), ("sort-score", "score")]:
            self.query_one(f"#{sid}", Button).variant = "primary" if label == sort else "default"
        self.action_refresh()

    # ── data loading ───────────────────────────────────────────────────────────

    @work(thread=True)
    def load_posts(self, tags: str = "", limit: int = 20) -> None:
        params: dict = {"limit": limit, "sort": self._sort}
        if tags.strip():
            params["tags"] = "+".join(tags.strip().split())
        try:
            data = api_get("/api/v1/library", params)
            posts = data.get("data") or []
        except Exception as e:
            self.app.call_from_thread(
                self.query_one("#lib-status", Label).update, f"[red]{e}[/red]"
            )
            return
        self.app.call_from_thread(self._populate, posts)

    def _populate(self, posts: list[dict]) -> None:
        self._posts = posts
        t = self.query_one("#library-table", DataTable)
        t.clear()
        for p in posts:
            size_kb = f"{(p.get('Size') or 0) // 1024} KB"
            tags = p.get("Tags") or []
            preview = ", ".join(tags[:4]) + ("…" if len(tags) > 4 else "")
            t.add_row(
                str(p.get("ID", "")),
                p.get("Source", ""),
                p.get("PostID", ""),
                p.get("Filetype", ""),
                size_kb,
                str(p.get("Score", 0)),
                preview,
            )
        self.query_one("#lib-status", Label).update(
            f"{len(posts)} posts  [dim]space=preview  r=refresh[/dim]"
        )
        if self._preview_open:
            self._load_preview_for_cursor()

    @on(Button.Pressed, "#search-btn")
    def on_search(self) -> None:
        self.action_refresh()

    def action_refresh(self) -> None:
        tags = self.query_one("#tag-input", Input).value
        try:
            limit = int(self.query_one("#limit-input", Input).value or "20")
        except ValueError:
            limit = 20
        self.load_posts(tags, limit)

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
        t = self.query_one("#library-table", DataTable)
        idx = t.cursor_row
        if idx < 0 or idx >= len(self._posts):
            return
        p = self._posts[idx]
        self.query_one("#image-preview", ImagePreview).load(
            p.get("ID", 0), p.get("Filetype", "")
        )


# ── Jobs Tab ───────────────────────────────────────────────────────────────────

class NewJobModal(ModalScreen[dict | None]):
    BINDINGS = [Binding("escape", "cancel", "Cancel")]

    def compose(self) -> ComposeResult:
        with Container(id="modal-container"):
            yield Static("[b]New Download Job[/b]", id="modal-title")
            yield Input(placeholder="tags (e.g. fox rating:safe)", id="job-tags")
            yield Input(placeholder="limit (default 20)", id="job-limit", value="20")
            yield Input(placeholder="sources (comma separated, empty = all)", id="job-sources")
            with Horizontal(id="modal-buttons"):
                yield Button("Create", variant="primary", id="create-btn")
                yield Button("Cancel", variant="default",  id="cancel-btn")

    @on(Button.Pressed, "#create-btn")
    def do_create(self) -> None:
        tags_raw = self.query_one("#job-tags", Input).value.strip()
        tags = tags_raw.split() if tags_raw else ["fox"]
        try:
            limit = int(self.query_one("#job-limit", Input).value or "20")
        except ValueError:
            limit = 20
        sources_raw = self.query_one("#job-sources", Input).value.strip()
        sources = [s.strip() for s in sources_raw.split(",") if s.strip()]
        payload: dict = {"tags": tags, "limit": limit}
        if sources:
            payload["sources"] = sources
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


def _fmt_progress(status: str, done: int, total: int, failed: int,
                   bar_width: int = 20, spinner: str = " ") -> str:
    if status == "pending":
        return "[dim]waiting…[/dim]"
    if status == "cancelled":
        return "[dim]cancelled[/dim]"
    spin = f"{spinner} "
    # always produce content as wide as a full bar so column width is stable
    empty = "░" * bar_width
    if total == 0:
        return f"{spin}[dim]{empty}[/dim] ?/?"
    filled = round(done / total * bar_width)
    bar = f"[green]{'█' * filled}[/green][dim]{'░' * (bar_width - filled)}[/dim]"
    fail = f" [red]{failed}✗[/red]" if failed else ""
    return f"{spin}{bar} [green]{done}[/green][dim]/{total}[/dim]{fail}"


class JobsTab(Widget):
    BINDINGS = [
        Binding("n", "new_job",    "New Job"),
        Binding("r", "refresh",    "Refresh"),
        Binding("x", "cancel_job", "Cancel Job"),
    ]

    def on_mount(self) -> None:
        self._streams: dict[str, dict] = {}
        t = self.query_one("#jobs-table", DataTable)
        bw = self._bar_width()
        t.add_column("ID",       key="id")
        t.add_column("Status",   key="status")
        t.add_column("Tags",     key="tags")
        t.add_column("Created",  key="created")
        # set explicit width so full bar fits from the start
        t.add_column("Progress", key="progress", width=bw + 8)
        self.set_interval(0.1, self._tick)
        self.load_jobs()

    def compose(self) -> ComposeResult:
        with Vertical():
            yield DataTable(id="jobs-table", cursor_type="row")
            yield Label("", id="jobs-status")

    def _bar_width(self) -> int:
        # columns before Progress ≈ 8+12+30+21 = 71, minus padding ~80 total
        return max(12, self.app.console.width - 80)

    def _tick(self) -> None:
        t = self.query_one("#jobs-table", DataTable)
        bw = self._bar_width()
        for rk, s in list(self._streams.items()):
            if not s["active"]:
                continue
            s["spin_idx"] = (s["spin_idx"] + 1) % len(_SPINNER)
            try:
                t.update_cell(rk, "progress",
                    _fmt_progress("streaming", s["done"], s["total"], s["failed"],
                                  bar_width=bw, spinner=_SPINNER[s["spin_idx"]]))
            except Exception:
                pass

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
        t = self.query_one("#jobs-table", DataTable)
        live = {k for k, s in self._streams.items() if s["active"]}
        t.clear()
        for j in jobs:
            self._add_row(t, j)
        for rk in live:
            s = self._streams[rk]
            self._add_stub(t, rk, s)
        total = len(jobs) + len(live)
        self.query_one("#jobs-status", Label).update(
            f"{total} jobs  [dim]n=new  x=cancel  r=refresh[/dim]"
        )

    def _add_row(self, t: DataTable, j: dict) -> None:
        status = j.get("status", "")
        color = STATUS_COLOR.get(status, "white")
        created = (j.get("created_at") or "")[:19].replace("T", " ")
        done, total, failed = j.get("done", 0), j.get("total", 0), j.get("failed", 0)
        t.add_row(
            j["id"][:8],
            f"[{color}]{status}[/{color}]",
            " ".join(j.get("tags") or [])[:28],
            created,
            _fmt_progress(status, done, total, failed, bar_width=self._bar_width()),
            key=j["id"],
        )

    def _add_stub(self, t: DataTable, rk: str, s: dict) -> None:
        t.add_row(
            rk[-8:],
            "[cyan]streaming[/cyan]",
            s["tags_str"],
            s["created"],
            _fmt_progress("streaming", s["done"], s["total"], s["failed"],
                          bar_width=self._bar_width(), spinner=_SPINNER[s["spin_idx"]]),
            key=rk,
        )

    def action_new_job(self) -> None:
        def handle(payload: dict | None) -> None:
            if payload:
                self._submit(payload)
        self.app.push_screen(NewJobModal(), handle)

    @work(thread=True)
    def _submit(self, payload: dict) -> None:
        import uuid as _uuid
        tags   = payload.get("tags", [])
        limit  = payload.get("limit", 20)
        sources = payload.get("sources", [])
        rk = f"stream-{_uuid.uuid4().hex[:8]}"
        s: dict = {
            "done": 0, "total": 0, "failed": 0,
            "spin_idx": 0, "active": True,
            "tags_str": " ".join(tags)[:28],
            "created": datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S"),
        }
        self._streams[rk] = s
        self.app.call_from_thread(self._insert_stub, rk, s)
        self._stream(tags, limit, sources, rk)

    def _insert_stub(self, rk: str, s: dict) -> None:
        self._add_stub(self.query_one("#jobs-table", DataTable), rk, s)

    def action_cancel_job(self) -> None:
        t = self.query_one("#jobs-table", DataTable)
        if t.cursor_row < 0 or not t.row_count:
            return
        rk, _ = t.coordinate_to_cell_key((t.cursor_row, 0))
        job_id = str(rk.value)
        if not job_id.startswith("stream-"):
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
    def _stream(self, tags: list[str], limit: int, sources: list[str], rk: str) -> None:
        s = self._streams[rk]
        params: dict = {"tags": "+".join(tags), "limit": str(limit)}
        if sources:
            params["sources"] = ",".join(sources)

        def set_final(status: str) -> None:
            color = STATUS_COLOR.get(status, "white")
            t = self.query_one("#jobs-table", DataTable)
            try:
                t.update_cell(rk, "status", f"[{color}]{status}[/{color}]")
                t.update_cell(rk, "progress",
                    _fmt_progress(status, s["done"], s["total"], s["failed"],
                                  bar_width=self._bar_width()))
            except Exception:
                pass

        try:
            with httpx.stream("GET", f"{BASE_URL}/api/v1/stream", params=params, timeout=None) as resp:
                resp.raise_for_status()
                for line in resp.iter_lines():
                    if not line.startswith("data: "):
                        continue
                    try:
                        ev = json.loads(line[6:])
                    except json.JSONDecodeError:
                        continue
                    t = ev.get("type")
                    if t == "found":
                        s["total"] = ev.get("count", 0) or 1
                    elif t == "downloaded":
                        s["done"] += 1
                    elif t == "failed":
                        s["failed"] += 1
                    elif t == "done":
                        s["done"]   = ev.get("done",   s["done"])
                        s["failed"] = ev.get("failed", s["failed"])
                        s["active"] = False
                        final = "done" if not s["failed"] else "failed"
                        self.app.call_from_thread(set_final, final)
                        self.app.call_from_thread(self.post_message, StreamCompleted())
                        break
        except Exception as e:
            s["active"] = False
            self.app.call_from_thread(
                self.query_one("#jobs-status", Label).update, f"[red]{e}[/red]"
            )


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
                yield Button("Check  [r]", id="check-btn")
                yield Button("Heal missing  [h]", variant="warning", id="heal-btn")
            yield Static("", id="heal-report")

    def on_mount(self) -> None:
        self.action_check()

    @on(Button.Pressed, "#check-btn")
    def action_check(self) -> None:
        self._do_check()

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
        total     = r.get("total", 0)
        healthy   = r.get("healthy", 0)
        missing   = r.get("missing", 0)
        corrupted = r.get("corrupted", 0)
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
        self.healing = v
        btn = self.query_one("#heal-btn", Button)
        btn.label   = "Healing…" if v else "Heal missing  [h]"
        btn.disabled = v

    def _show_heal(self, r: dict) -> None:
        missing   = r.get("missing", 0)
        corrupted = r.get("corrupted", 0)
        healed    = r.get("healed", 0)
        failed    = r.get("failed", 0)
        fc = "red" if failed else "green"
        self.query_one("#heal-report", Static).update(
            f"\n[b]Heal:[/b]  missing={missing}  corrupted={corrupted}"
            f"  [green]healed={healed}[/green]  [{fc}]failed={failed}[/{fc}]"
        )


# ── CSS ────────────────────────────────────────────────────────────────────────

CSS = """
/* Search + sort bars */
#search-bar {
    height: 3;
    padding: 0 1;
}
#search-bar Input { width: 1fr; margin-right: 1; }
#search-bar Button { width: 12; }

#sort-bar {
    height: 3;
    padding: 0 1;
    align: left middle;
}
#sort-label { margin-right: 1; color: $text-muted; }
#sort-bar Button { width: 10; margin-right: 1; }

/* Library layout */
#lib-content { height: 1fr; }
#lib-left    { width: 1fr; height: 1fr; }
#library-table { height: 1fr; }

#lib-status, #jobs-status {
    height: 1;
    padding: 0 1;
    color: $text-muted;
}

/* Jobs */
#jobs-table { height: 1fr; }

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
#health-inner  { padding: 2; }
#health-title  { margin-bottom: 1; }
#health-report { margin-bottom: 2; }
#health-buttons { height: 3; }
#health-buttons Button { margin-right: 1; }
#heal-report   { margin-top: 1; }

/* Tabs fill height */
TabPane { height: 1fr; }
LibraryTab, JobsTab, HealthTab { height: 1fr; }
"""


# ── App ────────────────────────────────────────────────────────────────────────

class FurLibApp(App):
    CSS = CSS
    TITLE = "FurLib TUI"
    BINDINGS = [Binding("q", "quit", "Quit")]

    def compose(self) -> ComposeResult:
        yield Header()
        with TabbedContent(initial="tab-library"):
            with TabPane("Library", id="tab-library"):
                yield LibraryTab()
            with TabPane("Jobs", id="tab-jobs"):
                yield JobsTab()
            with TabPane("Health", id="tab-health"):
                yield HealthTab()
        yield Footer()


if __name__ == "__main__":
    FurLibApp().run()
