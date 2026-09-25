package asnlookup

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDownloadASNDatabase_FirstDownload(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)

	changed, err := downloadASNDatabase(cacheFile, srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("downloadASNDatabase() error = %v", err)
	}
	if !changed {
		t.Error("expected changed = true on first download")
	}

	db, err := loadFromCache(cacheFile)
	if err != nil {
		t.Fatalf("loadFromCache() error = %v", err)
	}
	if len(db.IPv4Entries) != 2 || len(db.IPv6Entries) != 1 {
		t.Errorf("entries = %d/%d, want 2/1", len(db.IPv4Entries), len(db.IPv6Entries))
	}

	meta := readMeta(cacheFile)
	if meta == nil {
		t.Fatal("expected metadata sidecar to be written")
	}
	if meta.ETag != `"v1"` {
		t.Errorf("meta.ETag = %q, want %q", meta.ETag, `"v1"`)
	}
	if meta.FetchedAt.IsZero() {
		t.Error("expected meta.FetchedAt to be set")
	}
}

func TestDownloadASNDatabase_NotModified(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)

	if _, err := downloadASNDatabase(cacheFile, srv.URL, srv.Client()); err != nil {
		t.Fatalf("first download error = %v", err)
	}

	before, err := os.Stat(cacheFile)
	if err != nil {
		t.Fatalf("stat error = %v", err)
	}
	contentBefore, err := os.ReadFile(cacheFile)
	if err != nil {
		t.Fatalf("read error = %v", err)
	}

	changed, err := downloadASNDatabase(cacheFile, srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("second download error = %v", err)
	}
	if changed {
		t.Error("expected changed = false when upstream replies 304")
	}

	_, conds := srv.counts()
	if conds != 1 {
		t.Errorf("conditional requests = %d, want 1", conds)
	}

	after, err := os.Stat(cacheFile)
	if err != nil {
		t.Fatalf("stat error = %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("cache file was rewritten on a 304 response")
	}
	contentAfter, err := os.ReadFile(cacheFile)
	if err != nil {
		t.Fatalf("read error = %v", err)
	}
	if !bytes.Equal(contentBefore, contentAfter) {
		t.Error("cache file contents changed on a 304 response")
	}
}

func TestDownloadASNDatabase_ChangedETag(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)

	if _, err := downloadASNDatabase(cacheFile, srv.URL, srv.Client()); err != nil {
		t.Fatalf("first download error = %v", err)
	}

	srv.setBody(t, fixtureTSVv2, `"v2"`)

	changed, err := downloadASNDatabase(cacheFile, srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("second download error = %v", err)
	}
	if !changed {
		t.Fatal("expected changed = true when the ETag differs")
	}

	db, err := loadFromCache(cacheFile)
	if err != nil {
		t.Fatalf("loadFromCache() error = %v", err)
	}
	if len(db.IPv4Entries) != 3 {
		t.Errorf("IPv4 entries = %d, want 3", len(db.IPv4Entries))
	}

	if meta := readMeta(cacheFile); meta == nil || meta.ETag != `"v2"` {
		t.Errorf("expected metadata to hold the new ETag, got %+v", meta)
	}
}

func TestDownloadASNDatabase_HTTPError(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)
	srv.setStatus(http.StatusInternalServerError)

	changed, err := downloadASNDatabase(cacheFile, srv.URL, srv.Client())
	if err == nil {
		t.Fatal("expected an error for a 500 response")
	}
	if changed {
		t.Error("expected changed = false on error")
	}
	if _, statErr := os.Stat(cacheFile); statErr == nil {
		t.Error("expected no cache file to be created on error")
	}
}

func TestDownloadASNDatabase_UnwritableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions are not enforced")
	}

	tmpDir := t.TempDir()
	roDir := filepath.Join(tmpDir, "readonly")
	if err := os.Mkdir(roDir, 0500); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}
	cacheFile := filepath.Join(roDir, "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)

	if _, err := downloadASNDatabase(cacheFile, srv.URL, srv.Client()); err == nil {
		t.Fatal("expected an error for an unwritable cache directory")
	}

	entries, err := os.ReadDir(roDir)
	if err != nil {
		t.Fatalf("failed to read directory: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no leftover files, got %d", len(entries))
	}
}

func TestDownloadASNDatabase_NoMetaSidecar(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "asn.cache")
	writeCacheFixture(t, cacheFile, fixtureTSVv1)

	srv := newFixtureServer(t, fixtureTSVv2, `"v2"`)

	changed, err := downloadASNDatabase(cacheFile, srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("downloadASNDatabase() error = %v", err)
	}
	if !changed {
		t.Error("expected an unconditional download without a metadata sidecar")
	}

	_, conds := srv.counts()
	if conds != 0 {
		t.Errorf("conditional requests = %d, want 0", conds)
	}
	if meta := readMeta(cacheFile); meta == nil {
		t.Error("expected metadata to be written after the download")
	}
}

func TestReadMeta_MissingFile(t *testing.T) {
	cacheFile := filepath.Join(t.TempDir(), "asn.cache")

	if meta := readMeta(cacheFile); meta != nil {
		t.Errorf("readMeta() = %+v, want nil without a sidecar", meta)
	}
}

func TestReadMeta_CorruptJSON(t *testing.T) {
	cacheFile := filepath.Join(t.TempDir(), "asn.cache")
	if err := os.WriteFile(metaFileName(cacheFile), []byte("{not json"), 0644); err != nil {
		t.Fatalf("failed to write sidecar: %v", err)
	}

	if meta := readMeta(cacheFile); meta != nil {
		t.Errorf("readMeta() = %+v, want nil for corrupt JSON", meta)
	}
}

func TestReadMeta_NoValidators(t *testing.T) {
	cacheFile := filepath.Join(t.TempDir(), "asn.cache")
	if err := writeMeta(cacheFile, &cacheMeta{FetchedAt: time.Now()}); err != nil {
		t.Fatalf("writeMeta() error = %v", err)
	}

	if meta := readMeta(cacheFile); meta != nil {
		t.Errorf("readMeta() = %+v, want nil without an ETag or Last-Modified", meta)
	}
}

func TestWriteMeta_RoundTrip(t *testing.T) {
	cacheFile := filepath.Join(t.TempDir(), "asn.cache")
	fetchedAt := time.Now().UTC().Truncate(time.Second)
	want := &cacheMeta{
		ETag:         `"v1"`,
		LastModified: "Wed, 21 Oct 2020 07:28:00 GMT",
		FetchedAt:    fetchedAt,
	}

	if err := writeMeta(cacheFile, want); err != nil {
		t.Fatalf("writeMeta() error = %v", err)
	}

	got := readMeta(cacheFile)
	if got == nil {
		t.Fatal("readMeta() = nil, want the persisted metadata")
	}
	if got.ETag != want.ETag {
		t.Errorf("ETag = %q, want %q", got.ETag, want.ETag)
	}
	if got.LastModified != want.LastModified {
		t.Errorf("LastModified = %q, want %q", got.LastModified, want.LastModified)
	}
	if !got.FetchedAt.Equal(want.FetchedAt) {
		t.Errorf("FetchedAt = %s, want %s", got.FetchedAt, want.FetchedAt)
	}
}

func TestWriteMeta_UnwritableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions are not enforced")
	}

	roDir := filepath.Join(t.TempDir(), "readonly")
	if err := os.Mkdir(roDir, 0500); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	err := writeMeta(filepath.Join(roDir, "asn.cache"), &cacheMeta{ETag: `"v1"`})
	if err == nil {
		t.Fatal("expected an error for an unwritable directory")
	}
	if !strings.Contains(err.Error(), "failed to write cache metadata") {
		t.Errorf("error = %v, want it to mention the metadata write", err)
	}
}

func TestDownloadASNDatabase_IfModifiedSinceOnly(t *testing.T) {
	cacheFile := filepath.Join(t.TempDir(), "asn.cache")
	writeCacheFixture(t, cacheFile, fixtureTSVv1)
	if err := writeMeta(cacheFile, &cacheMeta{
		LastModified: "Wed, 21 Oct 2020 07:28:00 GMT",
		FetchedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("writeMeta() error = %v", err)
	}

	srv := newFixtureServer(t, fixtureTSVv2, "")

	if _, err := downloadASNDatabase(cacheFile, srv.URL, srv.Client()); err != nil {
		t.Fatalf("downloadASNDatabase() error = %v", err)
	}

	if _, conds := srv.counts(); conds != 0 {
		t.Errorf("If-None-Match requests = %d, want 0 without a known ETag", conds)
	}
	if got := srv.imsCount(); got != 1 {
		t.Errorf("If-Modified-Since requests = %d, want 1", got)
	}
}

func TestDownloadASNDatabase_MissingCacheSkipsValidators(t *testing.T) {
	cacheFile := filepath.Join(t.TempDir(), "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)

	if _, err := downloadASNDatabase(cacheFile, srv.URL, srv.Client()); err != nil {
		t.Fatalf("first download error = %v", err)
	}
	if readMeta(cacheFile) == nil {
		t.Fatal("expected a metadata sidecar after the first download")
	}

	// Drop the cache file but keep the sidecar: the validators describe data
	// that is no longer on disk, so they must not be sent.
	if err := os.Remove(cacheFile); err != nil {
		t.Fatalf("failed to remove cache file: %v", err)
	}

	changed, err := downloadASNDatabase(cacheFile, srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("second download error = %v", err)
	}
	if !changed {
		t.Error("expected changed = true when the cache file is gone")
	}
	if _, conds := srv.counts(); conds != 0 {
		t.Errorf("conditional requests = %d, want 0", conds)
	}
	if _, statErr := os.Stat(cacheFile); statErr != nil {
		t.Errorf("expected the cache file to be restored: %v", statErr)
	}
}

// errReader yields n bytes and then fails, simulating a connection dropped
// mid-transfer.
type errReader struct {
	n int
}

func (r *errReader) Read(p []byte) (int, error) {
	if r.n <= 0 {
		return 0, errors.New("connection reset by peer")
	}
	if len(p) > r.n {
		p = p[:r.n]
	}
	for i := range p {
		p[i] = 'x'
	}
	r.n -= len(p)
	return len(p), nil
}

func TestWriteCacheFile_ReaderError(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "asn.cache")

	err := writeCacheFile(cacheFile, &errReader{n: 1024})
	if err == nil {
		t.Fatal("expected an error when the body fails mid-stream")
	}
	if !strings.Contains(err.Error(), "failed to save cache file") {
		t.Errorf("error = %v, want it to mention the failed save", err)
	}

	if _, statErr := os.Stat(cacheFile); statErr == nil {
		t.Error("expected no cache file to be left behind")
	}
	entries, readErr := os.ReadDir(tmpDir)
	if readErr != nil {
		t.Fatalf("failed to read directory: %v", readErr)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected no leftover temporary files, got %v", names)
	}
}

func TestUpdateASNDatabase_Success(t *testing.T) {
	cacheFile := filepath.Join(t.TempDir(), "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)

	db, err := updateASNDatabase(cacheFile, srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("updateASNDatabase() error = %v", err)
	}
	if len(db.IPv4Entries) != 2 || len(db.IPv6Entries) != 1 {
		t.Errorf("entries = %d/%d, want 2/1", len(db.IPv4Entries), len(db.IPv6Entries))
	}
}

func TestUpdateASNDatabase_NotModifiedStillParses(t *testing.T) {
	cacheFile := filepath.Join(t.TempDir(), "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)

	if _, err := updateASNDatabase(cacheFile, srv.URL, srv.Client()); err != nil {
		t.Fatalf("first update error = %v", err)
	}

	db, err := updateASNDatabase(cacheFile, srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("second update error = %v", err)
	}
	if len(db.IPv4Entries) != 2 {
		t.Errorf("IPv4 entries = %d, want 2 from the unchanged cache", len(db.IPv4Entries))
	}
	if _, conds := srv.counts(); conds != 1 {
		t.Errorf("conditional requests = %d, want 1", conds)
	}
}

func TestUpdateASNDatabase_DownloadError(t *testing.T) {
	cacheFile := filepath.Join(t.TempDir(), "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)
	srv.setStatus(http.StatusInternalServerError)

	db, err := updateASNDatabase(cacheFile, srv.URL, srv.Client())
	if err == nil {
		t.Fatal("expected an error for a 500 response")
	}
	if db != nil {
		t.Error("expected a nil database on error")
	}
}

func TestDefaultHTTPClient_Timeout(t *testing.T) {
	client := defaultHTTPClient()
	if client == nil {
		t.Fatal("defaultHTTPClient() = nil")
	}
	if client.Timeout != defaultHTTPTimeout {
		t.Errorf("Timeout = %s, want %s", client.Timeout, defaultHTTPTimeout)
	}
	if defaultHTTPTimeout != 5*time.Minute {
		t.Errorf("defaultHTTPTimeout = %s, want 5m", defaultHTTPTimeout)
	}
}

func TestDownloadASNDatabase_NilClientUsesDefault(t *testing.T) {
	cacheFile := filepath.Join(t.TempDir(), "asn.cache")
	srv := newFixtureServer(t, fixtureTSVv1, `"v1"`)

	changed, err := downloadASNDatabase(cacheFile, srv.URL, nil)
	if err != nil {
		t.Fatalf("downloadASNDatabase() error = %v", err)
	}
	if !changed {
		t.Error("expected changed = true on first download")
	}
}

func TestDownloadASNDatabase_InvalidURL(t *testing.T) {
	cacheFile := filepath.Join(t.TempDir(), "asn.cache")

	if _, err := downloadASNDatabase(cacheFile, "://not a url", http.DefaultClient); err == nil {
		t.Fatal("expected an error for an unparsable URL")
	}
}

func TestMetaFileName(t *testing.T) {
	if got := metaFileName("/tmp/asn.cache"); got != "/tmp/asn.cache.meta" {
		t.Errorf("metaFileName() = %q, want %q", got, "/tmp/asn.cache.meta")
	}
}
