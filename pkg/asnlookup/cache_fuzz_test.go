package asnlookup

import (
	"bytes"
	"compress/gzip"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// gzipBytes compresses data without needing a *testing.T.
func gzipBytes(data []byte) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Write(data)
	gz.Close()
	return buf.Bytes()
}

// FuzzLoadFromCache feeds arbitrary TSV bytes through the gzip/TSV parser and
// asserts that it never panics and always returns sorted, well-formed entries.
func FuzzLoadFromCache(f *testing.F) {
	f.Add([]byte(fixtureTSVv1))
	f.Add([]byte(fixtureTSVv2))
	f.Add([]byte(""))
	f.Add([]byte("invalid line\n"))
	f.Add([]byte("short\tline\n"))
	f.Add([]byte("8.8.8.0\t8.8.8.255\tnotanumber\tUS\tGOOGLE\n"))
	f.Add([]byte("8.8.8.255\t8.8.8.0\t15169\tUS\tGOOGLE\n"))
	f.Add([]byte("::\tffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff\t0\t\t\n"))
	f.Add([]byte("1.0.0.0\t2001:db8::1\t13335\tUS\tMIXED\n"))
	f.Add([]byte("\t\t\t\t\n"))

	dir := f.TempDir()

	f.Fuzz(func(t *testing.T, tsv []byte) {
		cacheFile := filepath.Join(dir, "fuzz.cache")
		if err := os.WriteFile(cacheFile, gzipBytes(tsv), 0644); err != nil {
			t.Fatalf("failed to write cache file: %v", err)
		}

		db, err := loadFromCache(cacheFile)
		if err != nil {
			// A gzip-framed body is always readable; any error here must come
			// from the scanner (e.g. an over-long line), never from a panic.
			return
		}
		if db == nil {
			t.Fatal("loadFromCache() returned a nil database and a nil error")
		}

		assertSortedEntries(t, "IPv4", db.IPv4Entries, net.IPv4len)
		assertSortedEntries(t, "IPv6", db.IPv6Entries, net.IPv6len)

		// The lazy ASN index must stay consistent with the parsed entries.
		for _, entry := range db.IPv4Entries {
			if db.GetPrefixesByASN(entry.ASN) == nil {
				t.Fatalf("GetPrefixesByASN(%d) = nil, want a non-nil slice", entry.ASN)
			}
		}
	})
}

// assertSortedEntries checks the normalisation and ordering guarantees of a
// parsed entry slice.
func assertSortedEntries(t *testing.T, family string, entries []ASNEntry, wantLen int) {
	t.Helper()

	for i := range entries {
		if len(entries[i].Start) != wantLen {
			t.Fatalf("%s entry %d: Start has %d bytes, want %d", family, i, len(entries[i].Start), wantLen)
		}
		if len(entries[i].End) != wantLen {
			t.Fatalf("%s entry %d: End has %d bytes, want %d", family, i, len(entries[i].End), wantLen)
		}
		if i > 0 && bytes.Compare(entries[i-1].Start, entries[i].Start) > 0 {
			t.Fatalf("%s entries are not sorted at index %d: %s > %s",
				family, i, entries[i-1].Start, entries[i].Start)
		}
	}
}
