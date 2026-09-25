package asnlookup

import (
	"bufio"
	"compress/gzip"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFromCache_IPv4(t *testing.T) {
	// Create a temporary test cache file
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "test.cache")

	// Create test TSV data with IPv4
	testData := `1.0.0.0	1.0.0.255	13335	US	CLOUDFLARENET
8.8.8.0	8.8.8.255	15169	US	GOOGLE
192.168.1.0	192.168.1.255	64512	XX	PRIVATE-AS
`

	// Write gzipped test data
	f, err := os.Create(cacheFile)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	gzWriter := gzip.NewWriter(f)
	if _, err := gzWriter.Write([]byte(testData)); err != nil {
		t.Fatalf("failed to write test data: %v", err)
	}
	gzWriter.Close()
	f.Close()

	// Load from cache
	db, err := loadFromCache(cacheFile)
	if err != nil {
		t.Fatalf("loadFromCache() error = %v", err)
	}

	if db == nil {
		t.Fatal("loadFromCache() returned nil database")
	}

	// Verify IPv4 entries
	if len(db.IPv4Entries) != 3 {
		t.Errorf("expected 3 IPv4 entries, got %d", len(db.IPv4Entries))
	}

	if len(db.IPv6Entries) != 0 {
		t.Errorf("expected 0 IPv6 entries, got %d", len(db.IPv6Entries))
	}
}

func TestLoadFromCache_IPv6(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "test_ipv6.cache")

	// Create test TSV data with IPv6
	testData := `2001:4860:4860::	2001:4860:4860:ffff:ffff:ffff:ffff:ffff	15169	US	GOOGLE
2606:4700:4700::	2606:4700:4700:ffff:ffff:ffff:ffff:ffff	13335	US	CLOUDFLARENET
`

	f, err := os.Create(cacheFile)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	gzWriter := gzip.NewWriter(f)
	if _, err := gzWriter.Write([]byte(testData)); err != nil {
		t.Fatalf("failed to write test data: %v", err)
	}
	gzWriter.Close()
	f.Close()

	db, err := loadFromCache(cacheFile)
	if err != nil {
		t.Fatalf("loadFromCache() error = %v", err)
	}

	// Verify IPv6 entries
	if len(db.IPv6Entries) != 2 {
		t.Errorf("expected 2 IPv6 entries, got %d", len(db.IPv6Entries))
	}

	if len(db.IPv4Entries) != 0 {
		t.Errorf("expected 0 IPv4 entries, got %d", len(db.IPv4Entries))
	}
}

func TestLoadFromCache_Mixed(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "test_mixed.cache")

	// Create test TSV data with both IPv4 and IPv6
	testData := `1.0.0.0	1.0.0.255	13335	US	CLOUDFLARENET
8.8.8.0	8.8.8.255	15169	US	GOOGLE
2001:4860:4860::	2001:4860:4860:ffff:ffff:ffff:ffff:ffff	15169	US	GOOGLE
2606:4700:4700::	2606:4700:4700:ffff:ffff:ffff:ffff:ffff	13335	US	CLOUDFLARENET
`

	f, err := os.Create(cacheFile)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	gzWriter := gzip.NewWriter(f)
	if _, err := gzWriter.Write([]byte(testData)); err != nil {
		t.Fatalf("failed to write test data: %v", err)
	}
	gzWriter.Close()
	f.Close()

	db, err := loadFromCache(cacheFile)
	if err != nil {
		t.Fatalf("loadFromCache() error = %v", err)
	}

	// Verify both IPv4 and IPv6 entries
	if len(db.IPv4Entries) != 2 {
		t.Errorf("expected 2 IPv4 entries, got %d", len(db.IPv4Entries))
	}

	if len(db.IPv6Entries) != 2 {
		t.Errorf("expected 2 IPv6 entries, got %d", len(db.IPv6Entries))
	}
}

func TestLookupASN_IPv4(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "test.cache")

	testData := `1.0.0.0	1.0.0.255	13335	US	CLOUDFLARENET
8.8.8.0	8.8.8.255	15169	US	GOOGLE
192.168.1.0	192.168.1.255	64512	XX	PRIVATE-AS
`

	f, err := os.Create(cacheFile)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	gzWriter := gzip.NewWriter(f)
	gzWriter.Write([]byte(testData))
	gzWriter.Close()
	f.Close()

	db, err := loadFromCache(cacheFile)
	if err != nil {
		t.Fatalf("loadFromCache() error = %v", err)
	}

	tests := []struct {
		name        string
		ip          string
		wantASN     int
		wantCountry string
		wantFound   bool
	}{
		{
			name:        "Google DNS",
			ip:          "8.8.8.8",
			wantASN:     15169,
			wantCountry: "US",
			wantFound:   true,
		},
		{
			name:        "Cloudflare",
			ip:          "1.0.0.1",
			wantASN:     13335,
			wantCountry: "US",
			wantFound:   true,
		},
		{
			name:        "Private network",
			ip:          "192.168.1.100",
			wantASN:     64512,
			wantCountry: "XX",
			wantFound:   true,
		},
		{
			name:      "Not found",
			ip:        "10.0.0.1",
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			entry := db.LookupASN(ip)

			if tt.wantFound {
				if entry == nil {
					t.Errorf("expected to find entry for %s", tt.ip)
					return
				}
				if entry.GetASN() != tt.wantASN {
					t.Errorf("ASN = %d, want %d", entry.GetASN(), tt.wantASN)
				}
				if entry.GetCountryCode() != tt.wantCountry {
					t.Errorf("Country = %s, want %s", entry.GetCountryCode(), tt.wantCountry)
				}
			} else {
				if entry != nil {
					t.Errorf("expected no entry for %s, got ASN %d", tt.ip, entry.GetASN())
				}
			}
		})
	}
}

func TestLookupASN_IPv6(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "test_ipv6.cache")

	testData := `2001:4860:4860::	2001:4860:4860:ffff:ffff:ffff:ffff:ffff	15169	US	GOOGLE
2606:4700:4700::	2606:4700:4700:ffff:ffff:ffff:ffff:ffff	13335	US	CLOUDFLARENET
`

	f, err := os.Create(cacheFile)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	gzWriter := gzip.NewWriter(f)
	gzWriter.Write([]byte(testData))
	gzWriter.Close()
	f.Close()

	db, err := loadFromCache(cacheFile)
	if err != nil {
		t.Fatalf("loadFromCache() error = %v", err)
	}

	tests := []struct {
		name        string
		ip          string
		wantASN     int
		wantCountry string
		wantFound   bool
	}{
		{
			name:        "Google DNS IPv6",
			ip:          "2001:4860:4860::8888",
			wantASN:     15169,
			wantCountry: "US",
			wantFound:   true,
		},
		{
			name:        "Cloudflare DNS IPv6",
			ip:          "2606:4700:4700::1111",
			wantASN:     13335,
			wantCountry: "US",
			wantFound:   true,
		},
		{
			name:      "Not found",
			ip:        "2001:db8::1",
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			entry := db.LookupASN(ip)

			if tt.wantFound {
				if entry == nil {
					t.Errorf("expected to find entry for %s", tt.ip)
					return
				}
				if entry.GetASN() != tt.wantASN {
					t.Errorf("ASN = %d, want %d", entry.GetASN(), tt.wantASN)
				}
				if entry.GetCountryCode() != tt.wantCountry {
					t.Errorf("Country = %s, want %s", entry.GetCountryCode(), tt.wantCountry)
				}
			} else {
				if entry != nil {
					t.Errorf("expected no entry for %s, got ASN %d", tt.ip, entry.GetASN())
				}
			}
		})
	}
}

func TestLoadFromCache_InvalidFile(t *testing.T) {
	_, err := loadFromCache("/nonexistent/file.cache")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadFromCache_InvalidGzip(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "invalid.cache")

	// Write non-gzipped data
	if err := os.WriteFile(cacheFile, []byte("not gzipped"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, err := loadFromCache(cacheFile)
	if err == nil {
		t.Error("expected error for invalid gzip file")
	}
}

func TestLoadFromCache_MalformedTSV(t *testing.T) {
	tmpDir := t.TempDir()
	cacheFile := filepath.Join(tmpDir, "malformed.cache")

	// Create malformed TSV data (should skip these lines)
	testData := `invalid line
1.0.0.0	1.0.0.255	13335	US	CLOUDFLARENET
short	line
192.168.1.0	192.168.1.255	64512	XX	PRIVATE-AS
2001:4860:4860::	2001:4860:4860:ffff:ffff:ffff:ffff:ffff	15169	US	GOOGLE
`

	f, err := os.Create(cacheFile)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	gzWriter := gzip.NewWriter(f)
	if _, err := gzWriter.Write([]byte(testData)); err != nil {
		t.Fatalf("failed to write test data: %v", err)
	}
	gzWriter.Close()
	f.Close()

	db, err := loadFromCache(cacheFile)
	if err != nil {
		t.Fatalf("loadFromCache() error = %v", err)
	}

	// Should only load valid entries (2 IPv4, 1 IPv6)
	if len(db.IPv4Entries) != 2 {
		t.Errorf("expected 2 valid IPv4 entries, got %d", len(db.IPv4Entries))
	}
	if len(db.IPv6Entries) != 1 {
		t.Errorf("expected 1 valid IPv6 entry, got %d", len(db.IPv6Entries))
	}
}

func TestASNEntry_GetIPNet(t *testing.T) {
	tests := []struct {
		name       string
		start      string
		end        string
		wantPrefix string
	}{
		{
			name:       "IPv4 /24",
			start:      "192.168.1.0",
			end:        "192.168.1.255",
			wantPrefix: "192.168.1.0/24",
		},
		{
			name:       "IPv6 /64",
			start:      "2001:db8::",
			end:        "2001:db8::ffff:ffff:ffff:ffff",
			wantPrefix: "2001:db8::/64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := ASNEntry{
				Start:         net.ParseIP(tt.start),
				End:           net.ParseIP(tt.end),
				ASN:           12345,
				CountryCode:   "US",
				ASDescription: "TEST-AS",
			}

			ipNet := entry.GetIPNet()
			if ipNet.String() != tt.wantPrefix {
				t.Errorf("GetIPNet() = %s, want %s", ipNet.String(), tt.wantPrefix)
			}
		})
	}
}

func TestASNEntry_Methods(t *testing.T) {
	entry := ASNEntry{
		Start:         net.ParseIP("1.1.1.1"),
		End:           net.ParseIP("1.1.1.255"),
		ASN:           13335,
		CountryCode:   "US",
		ASDescription: "CLOUDFLARENET",
	}

	if entry.GetASN() != 13335 {
		t.Errorf("GetASN() = %d, want 13335", entry.GetASN())
	}

	if entry.GetCountryCode() != "US" {
		t.Errorf("GetCountryCode() = %s, want US", entry.GetCountryCode())
	}

	if entry.GetASDescription() != "CLOUDFLARENET" {
		t.Errorf("GetASDescription() = %s, want CLOUDFLARENET", entry.GetASDescription())
	}
}

func TestCalculatePrefixLength(t *testing.T) {
	tests := []struct {
		name  string
		start string
		end   string
		want  int
	}{
		{name: "single IPv4 address", start: "10.0.0.1", end: "10.0.0.1", want: 32},
		{name: "exact IPv4 /24", start: "192.168.1.0", end: "192.168.1.255", want: 24},
		{name: "non power of two range", start: "1.0.0.0", end: "1.0.0.200", want: 24},
		{name: "IPv6 /64", start: "2001:db8::", end: "2001:db8::ffff:ffff:ffff:ffff", want: 64},
		{name: "single IPv6 address", start: "2001:db8::1", end: "2001:db8::1", want: 128},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := net.ParseIP(tt.start)
			end := net.ParseIP(tt.end)
			if v4 := start.To4(); v4 != nil {
				start, end = v4, end.To4()
			}

			if got := calculatePrefixLength(start, end); got != tt.want {
				t.Errorf("calculatePrefixLength(%s, %s) = %d, want %d", tt.start, tt.end, got, tt.want)
			}
		})
	}
}

func TestLookupASN_EmptyDatabase(t *testing.T) {
	db := &ASNDatabase{}

	for _, ip := range []string{"8.8.8.8", "2001:4860:4860::8888"} {
		if entry := db.LookupASN(net.ParseIP(ip)); entry != nil {
			t.Errorf("LookupASN(%s) = %v, want nil on an empty database", ip, entry)
		}
	}
}

func TestLookupASN_GapBetweenRanges(t *testing.T) {
	db := newTestDB(t, "1.0.0.0\t1.0.0.255\t13335\tUS\tCLOUDFLARENET\n"+
		"8.8.8.0\t8.8.8.255\t15169\tUS\tGOOGLE\n")

	if entry := db.LookupASN(net.ParseIP("5.5.5.5")); entry != nil {
		t.Errorf("LookupASN(5.5.5.5) = AS%d, want nil for an address in a gap", entry.GetASN())
	}
}

func TestLookupASN_BelowFirstEntry(t *testing.T) {
	db := newTestDB(t, "8.8.8.0\t8.8.8.255\t15169\tUS\tGOOGLE\n")

	if entry := db.LookupASN(net.ParseIP("1.1.1.1")); entry != nil {
		t.Errorf("LookupASN(1.1.1.1) = AS%d, want nil below the first entry", entry.GetASN())
	}
}

func TestLookupASN_InvalidIP(t *testing.T) {
	db := newTestDB(t, "8.8.8.0\t8.8.8.255\t15169\tUS\tGOOGLE\n")

	for _, ip := range []net.IP{nil, {}, {1, 2, 3}, make(net.IP, 5)} {
		if entry := db.LookupASN(ip); entry != nil {
			t.Errorf("LookupASN(%v) = %v, want nil for a malformed address", []byte(ip), entry)
		}
	}
}

func TestLoadFromCache_EmptyFile(t *testing.T) {
	cacheFile := filepath.Join(t.TempDir(), "empty.cache")
	if err := os.WriteFile(cacheFile, nil, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	if _, err := loadFromCache(cacheFile); err == nil {
		t.Error("expected an error for an empty cache file")
	}
}

func TestLoadFromCache_TruncatedGzip(t *testing.T) {
	cacheFile := filepath.Join(t.TempDir(), "truncated.cache")
	body := gzipFixture(t, fixtureTSVv1)
	if err := os.WriteFile(cacheFile, body[:len(body)-8], 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	if _, err := loadFromCache(cacheFile); err == nil {
		t.Error("expected an error for a gzip stream truncated mid-read")
	}
}

func TestLoadFromCache_OverlongLine(t *testing.T) {
	// bufio.Scanner refuses tokens larger than 64 KiB; the parser must report
	// that as an error rather than silently returning a partial database.
	overlong := strings.Repeat("x", 70*1024)
	cacheFile := writeTestCache(t, "overlong.cache",
		"1.0.0.0\t1.0.0.255\t13335\tUS\tCLOUDFLARENET\n"+overlong+"\n")

	_, err := loadFromCache(cacheFile)
	if err == nil {
		t.Fatal("expected an error for a line exceeding the scanner limit")
	}
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Errorf("error = %v, want it to wrap bufio.ErrTooLong", err)
	}
}

func TestNewASNDatabaseWithCache_UsesExistingCache(t *testing.T) {
	cacheFile := writeTestCache(t, "existing.cache", fixtureTSVv1)

	db, err := NewASNDatabaseWithCache(cacheFile)
	if err != nil {
		t.Fatalf("NewASNDatabaseWithCache() error = %v", err)
	}
	if len(db.IPv4Entries) != 2 || len(db.IPv6Entries) != 1 {
		t.Errorf("entries = %d/%d, want 2/1", len(db.IPv4Entries), len(db.IPv6Entries))
	}

	entry := db.LookupASN(net.ParseIP("8.8.8.8"))
	if entry == nil || entry.GetASN() != 15169 {
		t.Errorf("LookupASN(8.8.8.8) = %v, want AS15169", entry)
	}
}

func TestNewASNDatabaseWithCache_CorruptCache(t *testing.T) {
	cacheFile := filepath.Join(t.TempDir(), "corrupt.cache")
	if err := os.WriteFile(cacheFile, []byte("not gzipped"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// The file exists, so no download is attempted and the parse error surfaces.
	if _, err := NewASNDatabaseWithCache(cacheFile); err == nil {
		t.Error("expected an error for an existing but unparsable cache file")
	}
}
