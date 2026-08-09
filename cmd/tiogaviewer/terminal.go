package main

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	"github.com/creack/pty"
)

// terminal runs a shell in a pty and captures its output. It is intentionally
// simple: ANSI escape sequences are stripped rather than interpreted, so it is a
// readable command log with line input, not a full terminal emulator.
type terminal struct {
	ptmx       *os.File
	cmd        *exec.Cmd
	invalidate func()

	mu  sync.Mutex
	buf []byte
}

func newTerminal(invalidate func()) *terminal {
	t := &terminal{invalidate: invalidate}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell, "-i")
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

func (t *terminal) send(line string) {
	if t.ptmx != nil {
		_, _ = t.ptmx.WriteString(line + "\n")
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
