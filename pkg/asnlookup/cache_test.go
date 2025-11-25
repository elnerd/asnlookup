package asnlookup

import (
	"compress/gzip"
	"net"
	"os"
	"path/filepath"
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
