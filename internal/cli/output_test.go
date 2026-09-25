package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/elnerd/asnlookup/pkg/asnlookup"
)

// testEntry is a hand-built lookup result, independent of any database.
var testEntry = asnlookup.ASNEntry{
	Start:         net.ParseIP("8.8.8.0").To4(),
	End:           net.ParseIP("8.8.8.255").To4(),
	ASN:           15169,
	CountryCode:   "US",
	ASDescription: "GOOGLE",
}

func TestPrintEntry_CSV(t *testing.T) {
	var buf bytes.Buffer

	if err := printEntry(&buf, testEntry, "csv"); err != nil {
		t.Fatalf("printEntry() error = %v", err)
	}

	want := "8.8.8.0/24, AS15169, US, \"GOOGLE\"\n"
	if buf.String() != want {
		t.Errorf("output = %q, want %q", buf.String(), want)
	}
}

func TestPrintEntry_JSON(t *testing.T) {
	var buf bytes.Buffer

	if err := printEntry(&buf, testEntry, "json"); err != nil {
		t.Fatalf("printEntry() error = %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON (%q): %v", buf.String(), err)
	}

	// The country code is lower-cased in JSON but not in CSV.
	if got["country"] != "us" {
		t.Errorf("country = %v, want us", got["country"])
	}
	if got["net"] != "8.8.8.0/24" {
		t.Errorf("net = %v, want 8.8.8.0/24", got["net"])
	}
	if got["as"] != float64(15169) {
		t.Errorf("as = %v, want 15169", got["as"])
	}
	if got["as_desc"] != "GOOGLE" {
		t.Errorf("as_desc = %v, want GOOGLE", got["as_desc"])
	}
}

func TestPrintEntry_UnknownFormat(t *testing.T) {
	var buf bytes.Buffer

	err := printEntry(&buf, testEntry, "yaml")
	if err == nil {
		t.Fatal("printEntry() = nil, want an error for an unknown format")
	}

	var ue usageError
	if !errors.As(err, &ue) {
		t.Errorf("error %v is not a usage error", err)
	}
	if err.Error() != "Error: unknown format 'yaml'. Use 'csv' or 'json'" {
		t.Errorf("error = %q", err.Error())
	}
	if buf.Len() != 0 {
		t.Errorf("output = %q, want nothing written", buf.String())
	}

	// The wrapped error stays reachable for callers that inspect the chain.
	if inner := errors.Unwrap(err); inner == nil || inner.Error() != err.Error() {
		t.Errorf("errors.Unwrap() = %v, want the wrapped message", inner)
	}
}

func TestPrintASNPrefixes_CSV(t *testing.T) {
	db := loadFixtureDB(t)

	var buf bytes.Buffer
	if err := printASNPrefixes(&buf, db, 15169, "csv"); err != nil {
		t.Fatalf("printASNPrefixes() error = %v", err)
	}

	want := "8.8.8.0/24, AS15169, US, \"GOOGLE\"\n" +
		"2001:4860:4860::/48, AS15169, US, \"GOOGLE\"\n"
	if buf.String() != want {
		t.Errorf("output = %q, want %q", buf.String(), want)
	}
}

func TestPrintASNPrefixes_JSON(t *testing.T) {
	db := loadFixtureDB(t)

	var buf bytes.Buffer
	if err := printASNPrefixes(&buf, db, 13335, "json"); err != nil {
		t.Fatalf("printASNPrefixes() error = %v", err)
	}

	var got struct {
		AS       int      `json:"as"`
		ASDesc   string   `json:"as_desc"`
		Country  string   `json:"country"`
		Prefixes []string `json:"prefixes"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON (%q): %v", buf.String(), err)
	}

	if got.AS != 13335 || got.ASDesc != "CLOUDFLARENET" || got.Country != "us" {
		t.Errorf("got %+v, want AS13335 CLOUDFLARENET us", got)
	}
	if len(got.Prefixes) != 1 || got.Prefixes[0] != "1.0.0.0/24" {
		t.Errorf("prefixes = %v, want [1.0.0.0/24]", got.Prefixes)
	}
}

func TestPrintASNPrefixes_NoPrefixes(t *testing.T) {
	db := loadFixtureDB(t)

	var buf bytes.Buffer
	err := printASNPrefixes(&buf, db, 64512, "csv")
	if err == nil {
		t.Fatal("printASNPrefixes() = nil, want an error for an unknown ASN")
	}

	var ue usageError
	if errors.As(err, &ue) {
		t.Error("an unknown ASN is a runtime error, not a usage error")
	}
	if err.Error() != "No prefixes found for ASN: 64512" {
		t.Errorf("error = %q", err.Error())
	}
	if buf.Len() != 0 {
		t.Errorf("output = %q, want nothing written", buf.String())
	}
}

func TestPrintASNPrefixes_UnknownFormat(t *testing.T) {
	db := loadFixtureDB(t)

	var buf bytes.Buffer
	// The format is rejected even though the ASN is unknown too.
	err := printASNPrefixes(&buf, db, 64512, "yaml")

	var ue usageError
	if !errors.As(err, &ue) {
		t.Fatalf("error %v is not a usage error", err)
	}
	if !strings.Contains(err.Error(), "unknown format 'yaml'") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestASNMetadata(t *testing.T) {
	db := loadFixtureDB(t)

	if country, desc := asnMetadata(db, 13335); country != "US" || desc != "CLOUDFLARENET" {
		t.Errorf("asnMetadata(13335) = (%q, %q), want (US, CLOUDFLARENET)", country, desc)
	}

	// An IPv6-only ASN is found through the second slice.
	if country, desc := asnMetadata(db, 15169); country != "US" || desc != "GOOGLE" {
		t.Errorf("asnMetadata(15169) = (%q, %q), want (US, GOOGLE)", country, desc)
	}

	if country, desc := asnMetadata(db, 64512); country != "" || desc != "" {
		t.Errorf("asnMetadata(64512) = (%q, %q), want empty values", country, desc)
	}
}

func TestCollectASNs(t *testing.T) {
	db := loadFixtureDB(t)

	asns := collectASNs(db)
	if len(asns) != 2 {
		t.Fatalf("collectASNs() = %v, want two distinct ASNs", asns)
	}

	seen := map[int]bool{}
	for _, asn := range asns {
		if seen[asn] {
			t.Errorf("ASN %d appears twice in %v", asn, asns)
		}
		seen[asn] = true
	}
	for _, want := range []int{13335, 15169} {
		if !seen[want] {
			t.Errorf("AS%d is missing from %v", want, asns)
		}
	}

	if got := collectASNs(nil); len(got) != 0 {
		t.Errorf("collectASNs(nil) = %v, want empty", got)
	}
}

// loadFixtureDB parses the shared TSV fixture into a database.
func loadFixtureDB(t *testing.T) *asnlookup.ASNDatabase {
	t.Helper()

	db, err := asnlookup.NewASNDatabaseWithCache(writeCacheFile(t, fixtureTSV))
	if err != nil {
		t.Fatalf("failed to load the fixture database: %v", err)
	}
	return db
}
