package main

import (
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/creack/pty"
)

// termScrollback caps how many lines of history the terminal keeps.
const termScrollback = 5000

// pmode is the escape-sequence parser state.
type pmode int

const (
	pNormal pmode = iota
	pEsc          // saw ESC
	pEscInt       // saw ESC ( / ) / * / + — consume the charset designator
	pCSI          // inside ESC [ …
	pOSC          // inside ESC ] …
	pOSCEsc       // saw ESC inside an OSC (looking for the ST terminator)
)

// terminal runs a shell in a pty and interprets its output into a grid of
// lines with a cursor. It is a small emulator: it honours carriage returns,
// backspaces, tabs and the common CSI cursor/erase codes so that shell line
// editing (echo, redraw, backspace) displays correctly. It does not implement
// scroll regions, colours or alternate screens.
type terminal struct {
	ptmx       *os.File
	cmd        *exec.Cmd
	invalidate func()
	home       string

	mu    sync.Mutex
	lines [][]rune
	row   int
	col   int
	cwd   string

	mode pmode
	csi  []byte
	osc  []byte
	utf  []byte
}

func newTerminal(invalidate func()) *terminal {
	t := &terminal{invalidate: invalidate}
	t.home, _ = os.UserHomeDir()
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell, "-i")
	// Apple's /etc/{zshrc,bashrc}_Apple_Terminal reports the working directory via
	// an OSC 7 escape on each prompt; we parse it to title the viewer.
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "TERM_PROGRAM=Apple_Terminal")
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.lines = [][]rune{[]rune("cannot start shell: " + err.Error())}
		return t
	}
	t.cmd = cmd
	t.ptmx = ptmx
	go t.readLoop()
	return t
}

func (t *terminal) readLoop() {
	b := make([]byte, 4096)
	for {
		n, err := t.ptmx.Read(b)
		if n > 0 {
			t.mu.Lock()
			t.feed(b[:n])
			t.mu.Unlock()
			if t.invalidate != nil {
				t.invalidate()
			}
		}
		if err != nil {
			return
		}
	}
}

// feed advances the emulator by the given raw bytes. Called with t.mu held.
func (t *terminal) feed(p []byte) {
	for _, b := range p {
		switch t.mode {
		case pNormal:
			switch {
			case b == 0x1b:
				t.flushUTF()
				t.mode = pEsc
			case b == '\r':
				t.flushUTF()
				t.col = 0
			case b == '\n':
				t.flushUTF()
				t.lineFeed()
			case b == '\b':
				t.flushUTF()
				if t.col > 0 {
					t.col--
				}
			case b == '\t':
				t.flushUTF()
				next := (t.col/8 + 1) * 8
				for t.col < next {
					t.putRune(' ')
				}
			case b >= 0x20 && b != 0x7f:
				t.utf = append(t.utf, b)
				if utf8.FullRune(t.utf) {
					r, _ := utf8.DecodeRune(t.utf)
					t.putRune(r)
					t.utf = t.utf[:0]
				} else if len(t.utf) >= utf8.UTFMax {
					t.putRune('?')
					t.utf = t.utf[:0]
				}
			}
		case pEsc:
			switch b {
			case '[':
				t.mode, t.csi = pCSI, t.csi[:0]
			case ']':
				t.mode, t.osc = pOSC, t.osc[:0]
			case '(', ')', '*', '+':
				t.mode = pEscInt
			default:
				t.mode = pNormal
			}
		case pEscInt:
			t.mode = pNormal
		case pCSI:
			if b >= 0x40 && b <= 0x7e {
				t.handleCSI(b)
				t.mode = pNormal
			} else {
				t.csi = append(t.csi, b)
			}
		case pOSC:
			switch b {
			case 0x07:
				t.handleOSC()
				t.mode = pNormal
			case 0x1b:
				t.mode = pOSCEsc
			default:
				t.osc = append(t.osc, b)
			}
		case pOSCEsc:
			t.handleOSC()
			t.mode = pNormal
		}
	}
}

func (t *terminal) flushUTF() {
	if len(t.utf) > 0 {
		r, _ := utf8.DecodeRune(t.utf)
		if r == utf8.RuneError {
			r = '?'
		}
		t.putRune(r)
		t.utf = t.utf[:0]
	}
}

func (t *terminal) ensureRow() {
	if t.row < 0 {
		t.row = 0
	}
	for t.row >= len(t.lines) {
		t.lines = append(t.lines, []rune{})
	}
}

// putRune writes r at the cursor (padding with spaces / overwriting as needed).
func (t *terminal) putRune(r rune) {
	t.ensureRow()
	line := t.lines[t.row]
	for len(line) < t.col {
		line = append(line, ' ')
	}
	if t.col < len(line) {
		line[t.col] = r
	} else {
		line = append(line, r)
	}
	t.lines[t.row] = line
	t.col++
}

func (t *terminal) lineFeed() {
	t.row++
	t.ensureRow()
	if len(t.lines) > termScrollback {
		drop := len(t.lines) - termScrollback
		t.lines = append([][]rune(nil), t.lines[drop:]...)
		t.row -= drop
		if t.row < 0 {
			t.row = 0
		}
	}
}

func (t *terminal) eraseLine(mode int) {
	t.ensureRow()
	line := t.lines[t.row]
	switch mode {
	case 0: // cursor to end
		if t.col < len(line) {
			t.lines[t.row] = line[:t.col]
		}
	case 1: // start to cursor
		for i := 0; i < t.col && i < len(line); i++ {
			line[i] = ' '
		}
	case 2: // whole line
		t.lines[t.row] = line[:0]
	}
}

func (t *terminal) eraseDisplay(mode int) {
	switch mode {
	case 0: // cursor to end of screen
		t.ensureRow()
		if t.col < len(t.lines[t.row]) {
			t.lines[t.row] = t.lines[t.row][:t.col]
		}
		t.lines = t.lines[:t.row+1]
	case 2, 3: // whole screen
		t.lines = [][]rune{{}}
		t.row, t.col = 0, 0
	}
}

func (t *terminal) handleCSI(final byte) {
	params := parseCSIParams(t.csi)
	p0 := 0
	if len(params) > 0 {
		p0 = params[0]
	}
	n := p0
	if n < 1 {
		n = 1
	}
	switch final {
	case 'C': // cursor forward
		t.col += n
	case 'D': // cursor back
		if t.col -= n; t.col < 0 {
			t.col = 0
		}
	case 'G': // cursor to column
		if t.col = p0 - 1; t.col < 0 {
			t.col = 0
		}
	case 'A': // cursor up
		if t.row -= n; t.row < 0 {
			t.row = 0
		}
	case 'B': // cursor down
		t.row += n
		t.ensureRow()
	case 'H', 'f': // cursor position — honour the column, keep the row to avoid
		// jumping around the scrollback grid.
		col := 1
		if len(params) >= 2 {
			col = params[1]
		}
		if t.col = col - 1; t.col < 0 {
			t.col = 0
		}
	case 'K': // erase in line
		t.eraseLine(p0)
	case 'J': // erase in display
		t.eraseDisplay(p0)
	}
	t.csi = t.csi[:0]
}

func parseCSIParams(b []byte) []int {
	s := strings.TrimPrefix(string(b), "?")
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ";")
	out := make([]int, len(parts))
	for i, p := range parts {
		out[i], _ = strconv.Atoi(p)
	}
	return out
}

// handleOSC parses the buffered OSC payload, extracting the cwd from OSC 7.
func (t *terminal) handleOSC() {
	s := string(t.osc)
	t.osc = t.osc[:0]
	if !strings.HasPrefix(s, "7;") {
		return
	}
	u := strings.TrimPrefix(s, "7;")
	i := strings.Index(u, "file://")
	if i < 0 {
		return
	}
	rest := u[i+len("file://"):]
	if slash := strings.IndexByte(rest, '/'); slash >= 0 {
		if p, err := url.PathUnescape(rest[slash:]); err == nil && p != "" {
			t.cwd = p
		}
	}
}

// dir returns the shell's current directory for the title (home shown as ~).
func (t *terminal) dir() string {
	t.mu.Lock()
	d := t.cwd
	t.mu.Unlock()
	if d == "" {
		return ""
	}
	if t.home != "" && (d == t.home || strings.HasPrefix(d, t.home+"/")) {
		return "~" + d[len(t.home):]
	}
	return d
}

// write sends raw bytes (a keystroke or control sequence) to the shell.
func (t *terminal) write(p string) {
	if t.ptmx != nil && p != "" {
		_, _ = t.ptmx.WriteString(p)
	}
}

func (t *terminal) close() {
	if t.ptmx != nil {
		t.ptmx.Close()
	}
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
	}
}

// snapshot returns the current screen lines and cursor position.
func (t *terminal) snapshot() (out []string, row, col int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.lines) == 0 {
		return []string{""}, 0, 0
	}
	out = make([]string, len(t.lines))
	for i, l := range t.lines {
		out[i] = string(l)
	}
	return out, t.row, t.col
}
