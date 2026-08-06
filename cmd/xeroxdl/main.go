// Command xeroxdl recursively mirrors a generated directory-listing archive
// (like https://xeroxparcarchive.computerhistory.org/_cdcsl_93-16_/1/.index.html),
// downloading every linked file under the starting directory.
package main

import (
	"flag"
	"os"
	"strings"
	"time"

	"cedarg/internal/mirror"
)

func main() {
	cfg := mirror.Config{SaveIndexes: true}
	flag.StringVar(&cfg.StartURL, "url", "https://xeroxparcarchive.computerhistory.org/_cdcsl_93-16_/1/.index.html", "starting .index.html URL")
	flag.StringVar(&cfg.OutDir, "out", "download", "local output directory")
	flag.IntVar(&cfg.Workers, "workers", 6, "number of concurrent download workers")
	flag.DurationVar(&cfg.Delay, "delay", 100*time.Millisecond, "polite delay between requests per worker")
	flag.IntVar(&cfg.Retries, "retries", 3, "download attempts per URL")
	flag.BoolVar(&cfg.SkipViews, "skip-views", false, "skip generated .html/.txt view files, keep only original files")
	flag.StringVar(&cfg.UserAgent, "ua", "xeroxdl/1.0 (+recursive archive mirror)", "User-Agent header")
	flag.DurationVar(&cfg.Timeout, "timeout", 60*time.Second, "per-request timeout")
	flag.BoolVar(&cfg.Verbose, "v", false, "verbose logging")
	only := flag.String("only", "", "comma-separated extensions to download (e.g. mesa,tioga); empty = all")
	flag.Parse()

	cfg.OnlyExts = parseExts(*only)

	if err := mirror.Run(cfg); err != nil {
		os.Exit(1)
	}
}

// parseExts splits a comma-separated extension list, trimming spaces and any
// leading dots, and dropping empties.
func parseExts(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(p), "."))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
