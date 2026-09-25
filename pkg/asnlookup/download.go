package asnlookup

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// defaultHTTPTimeout bounds a single download of the ASN database.
const defaultHTTPTimeout = 5 * time.Minute

// cacheMeta records the upstream validators of the cache file so a later
// request can be made conditional and skip the ~9 MB transfer.
type cacheMeta struct {
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"last_modified,omitempty"`
	FetchedAt    time.Time `json:"fetched_at"`
}

// metaFileName returns the path of the sidecar holding the cache metadata.
func metaFileName(cacheFile string) string {
	return cacheFile + ".meta"
}

// readMeta loads the metadata sidecar of cacheFile. A missing or unreadable
// sidecar simply yields nil, which makes the next request unconditional.
func readMeta(cacheFile string) *cacheMeta {
	data, err := os.ReadFile(metaFileName(cacheFile))
	if err != nil {
		return nil
	}

	meta := &cacheMeta{}
	if err := json.Unmarshal(data, meta); err != nil {
		return nil
	}
	if meta.ETag == "" && meta.LastModified == "" {
		return nil
	}
	return meta
}

// writeMeta persists the metadata sidecar next to cacheFile.
func writeMeta(cacheFile string, meta *cacheMeta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to encode cache metadata: %w", err)
	}
	if err := os.WriteFile(metaFileName(cacheFile), data, 0644); err != nil {
		return fmt.Errorf("failed to write cache metadata: %w", err)
	}
	return nil
}

// defaultHTTPClient returns the client used when the caller did not supply one.
func defaultHTTPClient() *http.Client {
	return &http.Client{Timeout: defaultHTTPTimeout}
}

// DownloadASNDatabase downloads the ASN database into cacheFile, using a
// conditional request when a previous download left an ETag or Last-Modified
// value behind. It reports whether the on-disk cache actually changed: an
// unchanged upstream file answers with 304 and costs a single small request.
func DownloadASNDatabase(cacheFile string) (bool, error) {
	return downloadASNDatabase(cacheFile, ASN_DATABASE_URL, defaultHTTPClient())
}

// downloadASNDatabase is the client- and URL-aware form of
// DownloadASNDatabase. The cache file is replaced atomically: the body is
// written to a temporary file in the same directory and renamed into place,
// so a failure can never leave a truncated cache behind.
func downloadASNDatabase(cacheFile, url string, client *http.Client) (bool, error) {
	if client == nil {
		client = defaultHTTPClient()
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return false, fmt.Errorf("failed to build request for ASN database: %w", err)
	}

	// Only send validators when the cache file they describe is still there.
	if _, statErr := os.Stat(cacheFile); statErr == nil {
		if meta := readMeta(cacheFile); meta != nil {
			if meta.ETag != "" {
				req.Header.Set("If-None-Match", meta.ETag)
			}
			if meta.LastModified != "" {
				req.Header.Set("If-Modified-Since", meta.LastModified)
			}
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to download ASN database: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotModified:
		return false, nil
	case http.StatusOK:
	default:
		return false, fmt.Errorf("failed to download ASN database: status %d", resp.StatusCode)
	}

	if err := writeCacheFile(cacheFile, resp.Body); err != nil {
		return false, err
	}

	meta := &cacheMeta{
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		FetchedAt:    time.Now(),
	}
	if err := writeMeta(cacheFile, meta); err != nil {
		return true, err
	}

	return true, nil
}

// writeCacheFile streams body into cacheFile atomically.
func writeCacheFile(cacheFile string, body io.Reader) error {
	dir := filepath.Dir(cacheFile)
	tmp, err := os.CreateTemp(dir, filepath.Base(cacheFile)+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create cache file: %w", err)
	}
	tmpName := tmp.Name()

	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}

	if _, err := io.Copy(tmp, body); err != nil {
		cleanup()
		return fmt.Errorf("failed to save cache file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("failed to save cache file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to save cache file: %w", err)
	}
	if err := os.Chmod(tmpName, 0644); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to save cache file: %w", err)
	}
	if err := os.Rename(tmpName, cacheFile); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to save cache file: %w", err)
	}

	return nil
}

// UpdateASNDatabase refreshes cacheFile with a conditional download and
// returns a freshly parsed database. The returned database is a complete,
// immutable snapshot; it is never partially populated.
func UpdateASNDatabase(cacheFile string) (*ASNDatabase, error) {
	return updateASNDatabase(cacheFile, ASN_DATABASE_URL, defaultHTTPClient())
}

// updateASNDatabase is the client- and URL-aware form of UpdateASNDatabase.
func updateASNDatabase(cacheFile, url string, client *http.Client) (*ASNDatabase, error) {
	if _, err := downloadASNDatabase(cacheFile, url, client); err != nil {
		return nil, err
	}
	return loadFromCache(cacheFile)
}
