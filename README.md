# cedar-g

A native-Go port of Rochus Keller's Cedar/Mesa **TiogaViewer** (originally
C++/Qt), plus a recursive downloader for the Xerox PARC archive it studies.

The viewer browses a Cedar source tree, decodes the Xerox **Tioga** editor file
format, renders documentation natively, and shows syntax-highlighted Cedar/Mesa
source. The GUI is built with [Gio](https://gioui.org) (pure-Go, immediate-mode),
which gives the pixel-precise 1-bit look and the custom tiled-window layout the
Cedar environment needs.

## Layout

```
cmd/tiogaviewer   Gio GUI: file tree + two-column tiled Viewers
cmd/xeroxdl       Recursive archive downloader (mirrors *.index.html trees)
cmd/xeroxsrc      Downloader variant: only *.mesa/*.tioga sources → download-src
internal/mirror   Shared recursive-crawler engine
internal/tioga    Tioga format decoder      (port of TiogaReader.cpp)
internal/cedar    Lexer, token types, and syntax highlighter
                  (port of CedarLexer/CedarToken/CedarTokenType/CedarHighlighter)
```

The GUI (`cmd/tiogaviewer`) is toolkit code only; the decoder, lexer and
highlighter under `internal/` are UI-agnostic and shared. (An earlier Fyne
implementation lives in git history before the Gio port.)

The Coco/R-generated Cedar parser from the original was left out: it was marked
work-in-progress by the author, is not compiled into the shipped GUI
(`HAVE_PARSER` is undefined), and its output is never displayed.

## Build & run

```bash
go build ./...
```

Run the viewer, optionally passing a source-tree root or a single file:

```bash
go run ./cmd/tiogaviewer /path/to/cedar/source/tree
```

With no argument it opens `./download-src` if that directory exists (the mirror
produced by `xeroxsrc`, see below).

Browse the **file tree** on the left (click `+`/`-` to expand directories, a file
to open it; the **Up** button re-roots to the parent). The tree lists `*.tioga`,
`*.mesa`, `*.df`, `*.require`, `*.profile`, `*.depends` files and their versioned
`!N` variants.

The workspace uses Cedar's **two-column tiled "Viewers"** model: files open as
bordered panes that **stack vertically within a column and partition its full
height** (no overlapping windows). A **global bar** runs across the top and a
reserved **icon tray** across the bottom. Each Viewer's header carries the Cedar
action buttons:

- **Destroy** — close it; the column reflows to reclaim the space.
- **Grow** — maximise it to the whole column (toggles back).
- **Icon** — minimise it to the bottom icon tray (click the icon to restore).
- **Switch** — move it to the other column.
- **Split** — open a second Viewer of the same file directly below.

The boundaries between stacked Viewers are draggable (proportional resize).
Opening an already-open file focuses it instead of duplicating.

The look is monochrome — white background, black text, thin black rules and
square 1px borders — after the Xerox Cedar / Tioga screen. Body text uses a serif
(system Georgia, falling back to DejaVu Serif); code uses a monospace. Syntax is
shown **without colour**, using **bold** (keywords) and *italic* (comments,
strings) only, in the spirit of Tioga's text "looks".

**Zoom** the whole UI with the global-bar buttons or `Cmd`/`Ctrl` `+`/`=`/`-`/`0`
(up to 6×).

## Downloader

Mirror the 1993 Solaris Cedar CD (or any subtree) locally:

```bash
go run ./cmd/xeroxdl -out mirror
go run ./cmd/xeroxdl -h    # flags: -url -workers -delay -skip-views -only -v ...
```

It follows every `.index.html`, stays under the starting directory, preserves
structure on disk, and shows a live progress line. Use `-only mesa,tioga` to
restrict to specific extensions.

**Source-only variant** — download just the Cedar sources and docs
(`*.mesa`/`*.tioga`, including versioned `!N` variants), skipping the generated
`.html`/`.txt` views and the index pages, into `download-src`:

```bash
go run ./cmd/xeroxsrc
```

Both downloaders share the crawler in `internal/mirror`.

## Tests

```bash
go test ./...
```

Covers the lexer, the highlighter (including multi-line `<< >>` comments and
`←`/Latin-1 column alignment), the Tioga decoder's fallbacks, and the viewer's
tiling logic (open/split/switch/minimize/restore/destroy and weight
partitioning). The GUI also has an offscreen render check gated behind the
`CAP_OUT` env var (uses Gio's `gpu/headless`).

## Notes on the port

- The 1,600-line generated token trie (`tokenTypeFromString`) is replaced by a
  keyword map plus a compact operator matcher with identical behaviour.
- The Tioga reader emits a small toolkit-agnostic block model (headings,
  paragraphs, code, quotes) instead of HTML, so the native GUI renders it
  directly; validated byte-for-byte against the archive's own `.tioga.txt`
  conversions.
- A latent out-of-bounds in the original's unterminated-comment path (column set
  to `-1`) is fixed.

## Credits & license

Original TiogaViewer © 2023 Rochus Keller, under LGPL 2.1/3 (viewer) and GPL 2/3
(highlighter, adapted from his Oberon project). Tioga format decoding derives
from Xerox Corporation (1993) sources. This Go port carries the same licenses;
see the `LICENSE.*` files under `Cedar/`.
```
