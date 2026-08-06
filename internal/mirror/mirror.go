// Package mirror recursively mirrors a generated directory-listing archive
// (like https://xeroxparcarchive.computerhistory.org/_cdcsl_93-16_/1/.index.html).
//
// Starting at a ".index.html" page it follows every subdirectory index page and
// downloads the linked files that live under the starting directory, preserving
// the directory structure on disk. An optional extension filter restricts which
// files are downloaded (index pages are always followed regardless).
package mirror

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Config controls a mirroring run.
type Config struct {
	StartURL  string        // starting .index.html URL
	OutDir    string        // local output directory
	Workers   int           // concurrent download workers
	Delay     time.Duration // polite delay per worker before each request
	Retries   int           // download attempts per URL
	UserAgent string        // User-Agent header
	Timeout   time.Duration // per-request timeout
	Verbose   bool          // log each downloaded file
	SkipViews bool          // skip generated .html/.txt view files
	// SaveIndexes controls whether the .index.html directory pages are written
	// to disk. They are always fetched to traverse the tree; set false to keep
	// them out of a source-only mirror.
	SaveIndexes bool
	// OnlyExts, when non-empty, restricts downloads to files whose name ends
	// with ".<ext>" or contains ".<ext>!" (a versioned variant). Extensions are
	// given without the leading dot, e.g. []string{"mesa","tioga"}.
	OnlyExts []string
}

// hrefRe extracts href attribute values from anchor tags. The archive's index
// pages are simple generated HTML, so a regex is sufficient and dependency-free.
var hrefRe = regexp.MustCompile(`(?i)<a\s+[^>]*href\s*=\s*["']([^"']+)["']`)

type job struct {
	u       *url.URL
	isIndex bool // true => an index page to parse, false => a file to download
}

type mirror struct {
	cfg      Config
	client   *http.Client
	basePath string // path prefix that scopes the crawl, e.g. /_cdcsl_93-16_/1/
	host     string

	wg   sync.WaitGroup
	sem  chan struct{}
	seen sync.Map // string(url) -> struct{}

	// Progress counters (atomic, read by the progress printer).
	indexesDone int64
	filesDone   int64
	bytesDone   int64
	active      int64

	mu       sync.Mutex
	failures []string
}

// Run performs the mirroring described by cfg. It returns an error if any URL
// failed to download.
func Run(cfg Config) error {
	if cfg.Workers < 1 {
		cfg.Workers = 1
	}
	start, err := url.Parse(cfg.StartURL)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	basePath := start.Path
	basePath = basePath[:strings.LastIndex(basePath, "/")+1]

	m := &mirror{
		cfg:      cfg,
		client:   &http.Client{Timeout: cfg.Timeout},
		basePath: basePath,
		host:     start.Host,
		sem:      make(chan struct{}, cfg.Workers),
	}

	fmt.Printf("Mirroring %s\n  scope:  host=%s path prefix=%s\n  output: %s\n",
		cfg.StartURL, m.host, m.basePath, cfg.OutDir)
	if len(cfg.OnlyExts) > 0 {
		fmt.Printf("  only:   %s\n", strings.Join(cfg.OnlyExts, ", "))
	}
	fmt.Println()

	stopProgress := make(chan struct{})
	progressDone := make(chan struct{})
	if !cfg.Verbose {
		go m.progressLoop(stopProgress, progressDone)
	}

	m.enqueue(job{u: start, isIndex: true})
	m.wg.Wait()

	if !cfg.Verbose {
		close(stopProgress)
		<-progressDone
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	fmt.Printf("\nDone. %d indexes, %d files, %s downloaded.\n",
		atomic.LoadInt64(&m.indexesDone), atomic.LoadInt64(&m.filesDone), humanBytes(atomic.LoadInt64(&m.bytesDone)))
	if len(m.failures) > 0 {
		fmt.Printf("%d failures:\n", len(m.failures))
		for _, f := range m.failures {
			fmt.Printf("  %s\n", f)
		}
		return fmt.Errorf("%d downloads failed", len(m.failures))
	}
	return nil
}

// enqueue schedules a job unless its URL was already handled.
func (m *mirror) enqueue(j job) {
	key := j.u.String()
	if _, loaded := m.seen.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.sem <- struct{}{}
		atomic.AddInt64(&m.active, 1)
		defer func() {
			atomic.AddInt64(&m.active, -1)
			<-m.sem
		}()
		if m.cfg.Delay > 0 {
			time.Sleep(m.cfg.Delay)
		}
		if j.isIndex {
			m.processIndex(j.u)
		} else {
			m.downloadFile(j.u)
		}
	}()
}

// inScope reports whether u belongs to the crawl (same host, under base path).
func (m *mirror) inScope(u *url.URL) bool {
	if u.Host != m.host {
		return false
	}
	return strings.HasPrefix(u.Path, m.basePath)
}

func (m *mirror) processIndex(u *url.URL) {
	body, err := m.fetch(u)
	if err != nil {
		m.recordFailure(u.String(), err)
		return
	}
	// Persist the index page itself so the mirror is complete (unless the
	// caller wants a source-only tree without the generated index pages).
	if m.cfg.SaveIndexes {
		m.save(u, body)
	}
	atomic.AddInt64(&m.indexesDone, 1)

	for _, mt := range hrefRe.FindAllStringSubmatch(string(body), -1) {
		raw := strings.TrimSpace(mt[1])
		if raw == "" || strings.HasPrefix(raw, "#") || strings.HasPrefix(raw, "mailto:") {
			continue
		}
		ref, err := url.Parse(raw)
		if err != nil {
			continue
		}
		target := u.ResolveReference(ref)
		target.Fragment = ""
		if !m.inScope(target) {
			continue // external link (e.g. computerhistory.org) or parent nav
		}
		if isIndexPage(target.Path) {
			m.enqueue(job{u: target, isIndex: true})
			continue
		}
		if m.cfg.SkipViews && isGeneratedView(target.Path) {
			continue
		}
		if len(m.cfg.OnlyExts) > 0 && !matchExt(target.Path, m.cfg.OnlyExts) {
			continue
		}
		m.enqueue(job{u: target, isIndex: false})
	}
}

func (m *mirror) downloadFile(u *url.URL) {
	body, err := m.fetch(u)
	if err != nil {
		m.recordFailure(u.String(), err)
		return
	}
	if err := m.save(u, body); err != nil {
		m.recordFailure(u.String(), err)
		return
	}
	atomic.AddInt64(&m.filesDone, 1)
	atomic.AddInt64(&m.bytesDone, int64(len(body)))
	if m.cfg.Verbose {
		fmt.Printf("  got %s (%s)\n", u.Path, humanBytes(int64(len(body))))
	}
}

// fetch retrieves a URL with retries and returns the body.
func (m *mirror) fetch(u *url.URL) ([]byte, error) {
	var lastErr error
	for attempt := 1; attempt <= m.cfg.Retries; attempt++ {
		req, err := http.NewRequest(http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", m.cfg.UserAgent)
		resp, err := m.client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(backoff(attempt))
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			time.Sleep(backoff(attempt))
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			if resp.StatusCode == http.StatusNotFound {
				break // 404s won't fix themselves
			}
			time.Sleep(backoff(attempt))
			continue
		}
		return body, nil
	}
	return nil, lastErr
}

// save writes the body to a local path derived from the URL, mapping the base
// path prefix to the output directory root.
func (m *mirror) save(u *url.URL, body []byte) error {
	rel := strings.TrimPrefix(u.Path, m.basePath)
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		rel = "index"
	}
	// Guard against path traversal from crafted links.
	local := filepath.Join(m.cfg.OutDir, filepath.FromSlash(path.Clean("/"+rel)))
	cleanOut := filepath.Clean(m.cfg.OutDir)
	if !strings.HasPrefix(local, cleanOut+string(os.PathSeparator)) && local != cleanOut {
		return fmt.Errorf("refusing to write outside output dir: %s", local)
	}
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		return err
	}
	return os.WriteFile(local, body, 0o644)
}

func (m *mirror) recordFailure(u string, err error) {
	m.mu.Lock()
	m.failures = append(m.failures, fmt.Sprintf("%s: %v", u, err))
	m.mu.Unlock()
	// \r\033[K clears the in-place progress line before logging.
	fmt.Fprintf(os.Stderr, "\r\033[KFAIL %s: %v\n", u, err)
}

// progressLoop prints a live, in-place status line until stop is closed.
func (m *mirror) progressLoop(stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	spin := []rune(`|/-\`)
	i := 0
	render := func() {
		fmt.Fprintf(os.Stderr, "\r\033[K%c  indexes: %d  files: %d  downloaded: %s  active: %d",
			spin[i%len(spin)],
			atomic.LoadInt64(&m.indexesDone),
			atomic.LoadInt64(&m.filesDone),
			humanBytes(atomic.LoadInt64(&m.bytesDone)),
			atomic.LoadInt64(&m.active))
		i++
	}
	for {
		select {
		case <-stop:
			render()
			fmt.Fprintln(os.Stderr)
			return
		case <-ticker.C:
			render()
		}
	}
}

// isIndexPage reports whether the path is a directory listing to recurse into.
func isIndexPage(p string) bool {
	return path.Base(p) == ".index.html"
}

// isGeneratedView reports whether a path is one of the archive's generated
// rendered views (e.g. .README.html, .README~.txt) rather than an original file.
func isGeneratedView(p string) bool {
	b := path.Base(p)
	if strings.HasPrefix(b, ".") && (strings.HasSuffix(b, ".html") || strings.HasSuffix(b, ".txt")) {
		return true
	}
	return false
}

// matchExt reports whether the path's base name ends with ".<ext>" or contains
// ".<ext>!" for any of the given extensions (given without the leading dot).
func matchExt(p string, exts []string) bool {
	b := path.Base(p)
	for _, e := range exts {
		if strings.HasSuffix(b, "."+e) || strings.Contains(b, "."+e+"!") {
			return true
		}
	}
	return false
}

func backoff(attempt int) time.Duration {
	return time.Duration(attempt) * 500 * time.Millisecond
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
