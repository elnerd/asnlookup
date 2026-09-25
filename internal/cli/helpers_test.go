package cli

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const fixtureTSV = "1.0.0.0\t1.0.0.255\t13335\tUS\tCLOUDFLARENET\n" +
	"8.8.8.0\t8.8.8.255\t15169\tUS\tGOOGLE\n" +
	"2001:4860:4860::\t2001:4860:4860:ffff:ffff:ffff:ffff:ffff\t15169\tUS\tGOOGLE\n"

const fixtureTSVUpdated = fixtureTSV +
	"9.9.9.0\t9.9.9.255\t19281\tUS\tQUAD9\n"

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

// writeCacheFile writes a gzipped fixture into a fresh temporary directory and
// returns its path.
func writeCacheFile(t *testing.T, tsv string) string {
	t.Helper()

	cacheFile := filepath.Join(t.TempDir(), "asn.cache")
	if err := os.WriteFile(cacheFile, gzipFixture(t, tsv), 0644); err != nil {
		t.Fatalf("failed to write cache fixture: %v", err)
	}
	return cacheFile
}

// newFixtureServer serves a gzipped TSV body with a fixed ETag.
func newFixtureServer(t *testing.T, tsv, etag string) *httptest.Server {
	t.Helper()

	body := gzipFixture(t, tsv)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == etag {
			w.Header().Set("ETag", etag)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Type", "application/gzip")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// runCLI executes Run with captured output.
func runCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()

	var out, errOut bytes.Buffer
	code = Run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}
