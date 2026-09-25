package asnlookup

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
)

const fixtureTSVv1 = `1.0.0.0	1.0.0.255	13335	US	CLOUDFLARENET
8.8.8.0	8.8.8.255	15169	US	GOOGLE
2001:4860:4860::	2001:4860:4860:ffff:ffff:ffff:ffff:ffff	15169	US	GOOGLE
`

const fixtureTSVv2 = `1.0.0.0	1.0.0.255	13335	US	CLOUDFLARENET
8.8.8.0	8.8.8.255	15169	US	GOOGLE
9.9.9.0	9.9.9.255	19281	US	QUAD9
2001:4860:4860::	2001:4860:4860:ffff:ffff:ffff:ffff:ffff	15169	US	GOOGLE
`

// gzipFixture compresses tsv the way the upstream database is served.
func gzipFixture(t *testing.T, tsv string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write([]byte(tsv)); err != nil {
		t.Fatalf("failed to gzip fixture: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("failed to close gzip writer: %v", err)
	}
	return buf.Bytes()
}

// writeCacheFixture writes a gzipped fixture straight to a cache file.
func writeCacheFixture(t *testing.T, cacheFile, tsv string) {
	t.Helper()

	if err := os.WriteFile(cacheFile, gzipFixture(t, tsv), 0644); err != nil {
		t.Fatalf("failed to write cache fixture: %v", err)
	}
}

// writeTestCache writes the given TSV data as a gzipped cache file in a fresh
// temporary directory and returns its path.
func writeTestCache(t *testing.T, name, data string) string {
	t.Helper()

	cacheFile := filepath.Join(t.TempDir(), name)
	writeCacheFixture(t, cacheFile, data)
	return cacheFile
}

// newTestDB parses tsv into a database through the regular cache path.
func newTestDB(t *testing.T, tsv string) *ASNDatabase {
	t.Helper()

	db, err := loadFromCache(writeTestCache(t, "test.cache", tsv))
	if err != nil {
		t.Fatalf("loadFromCache failed: %v", err)
	}
	return db
}

func prefixStrings(prefixes []netip.Prefix) []string {
	out := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		out = append(out, p.String())
	}
	return out
}

func assertPrefixes(t *testing.T, got []netip.Prefix, expected []string) {
	t.Helper()

	gotStrings := prefixStrings(got)
	if len(gotStrings) != len(expected) {
		t.Fatalf("got %v, expected %v", gotStrings, expected)
	}
	for i := range gotStrings {
		if gotStrings[i] != expected[i] {
			t.Errorf("prefix %d: got %s, expected %s", i, gotStrings[i], expected[i])
		}
	}
}

func mustAddr(t *testing.T, s string) netip.Addr {
	t.Helper()
	a, err := netip.ParseAddr(s)
	if err != nil {
		t.Fatalf("invalid test address %q: %v", s, err)
	}
	return a
}

// fixtureServer serves a gzipped TSV body with an ETag and honours
// If-None-Match. The served body can be swapped at runtime.
type fixtureServer struct {
	*httptest.Server

	mu           chan struct{}
	body         []byte
	etag         string
	lastModified string
	requests     int
	conds        int
	ims          int
	status       int
}

func newFixtureServer(t *testing.T, tsv, etag string) *fixtureServer {
	t.Helper()

	fs := &fixtureServer{
		mu:           make(chan struct{}, 1),
		body:         gzipFixture(t, tsv),
		etag:         etag,
		lastModified: "Wed, 21 Oct 2020 07:28:00 GMT",
	}
	fs.Server = httptest.NewServer(http.HandlerFunc(fs.handle))
	t.Cleanup(fs.Close)
	return fs
}

func (fs *fixtureServer) lock()   { fs.mu <- struct{}{} }
func (fs *fixtureServer) unlock() { <-fs.mu }

func (fs *fixtureServer) handle(w http.ResponseWriter, r *http.Request) {
	fs.lock()
	body := fs.body
	etag := fs.etag
	lastModified := fs.lastModified
	status := fs.status
	fs.requests++
	inm := r.Header.Get("If-None-Match")
	if inm != "" {
		fs.conds++
	}
	if r.Header.Get("If-Modified-Since") != "" {
		fs.ims++
	}
	fs.unlock()

	if status != 0 {
		w.WriteHeader(status)
		return
	}

	if etag != "" {
		w.Header().Set("ETag", etag)
	}
	if lastModified != "" {
		w.Header().Set("Last-Modified", lastModified)
	}

	if inm != "" && inm == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", "application/gzip")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

func (fs *fixtureServer) setBody(t *testing.T, tsv, etag string) {
	t.Helper()

	fs.lock()
	defer fs.unlock()
	fs.body = gzipFixture(t, tsv)
	fs.etag = etag
}

// setETag replaces only the ETag served, leaving the body untouched. An empty
// value suppresses the header entirely.
func (fs *fixtureServer) setETag(etag string) {
	fs.lock()
	defer fs.unlock()
	fs.etag = etag
}

func (fs *fixtureServer) setStatus(status int) {
	fs.lock()
	defer fs.unlock()
	fs.status = status
}

func (fs *fixtureServer) counts() (requests, conds int) {
	fs.lock()
	defer fs.unlock()
	return fs.requests, fs.conds
}

// imsCount returns the number of requests carrying an If-Modified-Since header.
func (fs *fixtureServer) imsCount() int {
	fs.lock()
	defer fs.unlock()
	return fs.ims
}
