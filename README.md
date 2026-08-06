# cedar-g

A native-Go port of Rochus Keller's Cedar/Mesa **TiogaViewer** (originally
C++/Qt), plus a recursive downloader for the Xerox PARC archive it studies.

The viewer browses a Cedar source tree, decodes the Xerox **Tioga** editor file
format, renders documentation natively, and shows syntax-highlighted Cedar/Mesa
source. The GUI is built with [Fyne](https://fyne.io) (pure-Go, cross-platform).

## Layout

```
cmd/tiogaviewer   Fyne GUI: file tree + document/code viewers
cmd/xeroxdl       Recursive archive downloader (mirrors *.index.html trees)
cmd/xeroxsrc      Downloader variant: only *.mesa/*.tioga sources → download-src
internal/mirror   Shared recursive-crawler engine
internal/tioga    Tioga format decoder      (port of TiogaReader.cpp)
internal/cedar    Lexer, token types, and syntax highlighter
                  (port of CedarLexer/CedarToken/CedarTokenType/CedarHighlighter)
```

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

Inside the app use **File → Open Directory** (Ctrl+O) or **Open File**. The tree
lists `*.tioga`, `*.mesa`, `*.df`, `*.require`, `*.profile`, `*.depends` files
and their versioned `!N` variants.

**Zoom** (whole-UI font size) via the **View** menu or keyboard — `Cmd`/`Alt`
plus `+`/`=` to enlarge, `-` to shrink, and `0` to reset.

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
`←`/Latin-1 column alignment), the Tioga decoder's fallbacks, and a headless
smoke test of the GUI render paths.

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
