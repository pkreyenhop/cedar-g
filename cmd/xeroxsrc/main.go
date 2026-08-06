// Command xeroxsrc is a variation of xeroxdl that mirrors only the Cedar source
// and documentation originals — *.mesa and *.tioga files (and their versioned
// !N variants) — skipping the generated .html/.txt views and everything else.
// It writes to ./download-src by default.
package main

import (
	"flag"
	"os"
	"time"

	"cedarg/internal/mirror"
)

func main() {
	cfg := mirror.Config{OnlyExts: []string{"mesa", "tioga"}}
	flag.StringVar(&cfg.StartURL, "url", "https://xeroxparcarchive.computerhistory.org/_cdcsl_93-16_/1/.index.html", "starting .index.html URL")
	flag.StringVar(&cfg.OutDir, "out", "download-src", "local output directory")
	flag.IntVar(&cfg.Workers, "workers", 6, "number of concurrent download workers")
	flag.DurationVar(&cfg.Delay, "delay", 100*time.Millisecond, "polite delay between requests per worker")
	flag.IntVar(&cfg.Retries, "retries", 3, "download attempts per URL")
	flag.StringVar(&cfg.UserAgent, "ua", "xeroxsrc/1.0 (+recursive archive mirror)", "User-Agent header")
	flag.DurationVar(&cfg.Timeout, "timeout", 60*time.Second, "per-request timeout")
	flag.BoolVar(&cfg.Verbose, "v", false, "verbose logging")
	flag.Parse()

	if err := mirror.Run(cfg); err != nil {
		os.Exit(1)
	}
}
