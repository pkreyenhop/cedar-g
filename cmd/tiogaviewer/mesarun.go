package main

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"time"

	"cedarg/internal/mesa"
)

const (
	mesaRunTimeout = 3 * time.Second
	mesaMaxOutput  = 64 * 1024
)

// runMesa parses and evaluates Mesa source, returning the program's output plus
// any parse/runtime error as text. It runs the interpreter on a separate
// goroutine with a timeout so a runaway program cannot hang the UI. (On timeout
// that goroutine is abandoned — the interpreter has no cancellation — which is
// acceptable for an occasional runaway.)
func runMesa(src string) string {
	m, err := mesa.ParseSource(src)
	if err != nil {
		return "parse error: " + err.Error()
	}

	// Surface a safety advisory when the program relies on unsafe features or a
	// low-level runtime the safe interpreter cannot faithfully reproduce.
	var prefix string
	if note := mesa.AnalyzeSafety(src, m).Note(); note != "" {
		prefix = note + "\n"
	}

	buf := &boundedBuffer{max: mesaMaxOutput}
	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("panic: %v", r)
			}
		}()
		done <- mesa.NewInterp(buf).Run(m)
	}()

	select {
	case err := <-done:
		out := buf.String()
		if err != nil {
			if out != "" && !strings.HasSuffix(out, "\n") {
				out += "\n"
			}
			out += "error: " + err.Error()
		}
		if out == "" {
			out = "(no output)"
		}
		return prefix + out
	case <-time.After(mesaRunTimeout):
		out := buf.String()
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		return prefix + out + fmt.Sprintf("… timed out after %s (possible infinite loop)", mesaRunTimeout)
	}
}

// boundedBuffer is a thread-safe writer that caps how much output it retains.
type boundedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := b.max - b.buf.Len(); room > 0 {
		if len(p) > room {
			b.buf.Write(p[:room])
		} else {
			b.buf.Write(p)
		}
	}
	return len(p), nil // pretend the whole write succeeded to avoid short-write errors
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
