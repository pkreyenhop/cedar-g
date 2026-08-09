package main

import (
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	"github.com/creack/pty"
)

// terminal runs a shell in a pty and captures its output. It is intentionally
// simple: ANSI escape sequences are stripped rather than interpreted, so it is a
// readable log of a real interactive shell, not a full terminal emulator.
type terminal struct {
	ptmx       *os.File
	cmd        *exec.Cmd
	invalidate func()
	home       string

	mu     sync.Mutex
	buf    []byte
	rawAcc []byte // recent raw bytes, scanned for the cwd (OSC 7) sequence
	cwd    string
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
		t.buf = []byte("cannot start shell: " + err.Error() + "\n")
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
			clean := stripANSI(b[:n])
			t.mu.Lock()
			t.scanCwd(b[:n])
			t.buf = append(t.buf, clean...)
			if len(t.buf) > 200000 { // cap the scrollback
				t.buf = t.buf[len(t.buf)-200000:]
			}
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

// osc7Re matches OSC 7 cwd reports: ESC ] 7 ; file://host/PATH (BEL or ST).
var osc7Re = regexp.MustCompile(`\x1b\]7;file://[^/]*([^\x07\x1b]*)(?:\x07|\x1b\\)`)

// scanCwd updates t.cwd from any OSC 7 sequence in the raw stream. Called with
// t.mu held.
func (t *terminal) scanCwd(raw []byte) {
	t.rawAcc = append(t.rawAcc, raw...)
	if m := osc7Re.FindAllSubmatch(t.rawAcc, -1); len(m) > 0 {
		if p, err := url.PathUnescape(string(m[len(m)-1][1])); err == nil && p != "" {
			t.cwd = p
		}
	}
	if len(t.rawAcc) > 4096 { // keep a small tail to catch split sequences
		t.rawAcc = t.rawAcc[len(t.rawAcc)-4096:]
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

// lines returns the current output split into display lines (tabs expanded).
func (t *terminal) lines() []string {
	t.mu.Lock()
	s := string(t.buf)
	t.mu.Unlock()
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	raw := strings.Split(s, "\n")
	out := make([]string, len(raw))
	for i, l := range raw {
		out[i] = expandTabs(l)
	}
	return out
}

// ansiRe matches ANSI/VT escape sequences and control characters (keeping tab,
// newline and carriage return, which lines() handles).
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]` +
	`|\x1b[()][0-9A-Za-z]` +
	`|\x1b[=>]` +
	`|\x1b\][^\x07]*\x07` +
	`|[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]`)

func stripANSI(b []byte) []byte {
	return ansiRe.ReplaceAll(b, nil)
}
