package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/elnerd/asnlookup/pkg/asnlookup"
)

// unknownFormatError builds the usage error reported for an unsupported
// --format value.
func unknownFormatError(format string) error {
	return usageError{fmt.Errorf("Error: unknown format '%s'. Use 'csv' or 'json'", format)}
}

// printEntry renders a single lookup result in the requested format.
func printEntry(w io.Writer, entry asnlookup.ASNEntryInterface, format string) error {
	ipNet := entry.GetIPNet()

	switch strings.ToLower(format) {
	case "json":
		result := map[string]interface{}{
			"net":     ipNet.String(),
			"as":      entry.GetASN(),
			"country": strings.ToLower(entry.GetCountryCode()),
			"as_desc": entry.GetASDescription(),
		}
		jsonBytes, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("Error marshaling JSON: %w", err)
		}
		fmt.Fprintln(w, string(jsonBytes))
	case "csv":
		fmt.Fprintf(w, "%s, AS%d, %s, \"%s\"\n",
			ipNet.String(),
			entry.GetASN(),
			entry.GetCountryCode(),
			entry.GetASDescription())
	default:
		return unknownFormatError(format)
	}

	return nil
}

// printASNPrefixes renders every prefix announced by asn in the requested
// format.
func printASNPrefixes(w io.Writer, db *asnlookup.ASNDatabase, asn int, format string) error {
	// Validate the format before the lookup so an unsupported value is
	// reported even when the ASN happens to be unknown.
	switch strings.ToLower(format) {
	case "json", "csv":
	default:
		return unknownFormatError(format)
	}

	prefixes := db.GetPrefixesByASN(asn)
	if len(prefixes) == 0 {
		return fmt.Errorf("No prefixes found for ASN: %d", asn)
	}

	countryCode, description := asnMetadata(db, asn)

	if strings.ToLower(format) == "json" {
		prefixStrings := make([]string, 0, len(prefixes))
		for _, prefix := range prefixes {
			prefixStrings = append(prefixStrings, prefix.String())
		}
		result := map[string]interface{}{
			"as":       asn,
			"as_desc":  description,
			"country":  strings.ToLower(countryCode),
			"prefixes": prefixStrings,
		}
		jsonBytes, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("Error marshaling JSON: %w", err)
		}
		fmt.Fprintln(w, string(jsonBytes))
		return nil
	}

	for _, prefix := range prefixes {
		fmt.Fprintf(w, "%s, AS%d, %s, \"%s\"\n", prefix.String(), asn, countryCode, description)
	}
	return nil
}

// asnMetadata returns the country code and description of the first entry
// matching the given ASN.
func asnMetadata(db *asnlookup.ASNDatabase, asn int) (string, string) {
	for _, entries := range [][]asnlookup.ASNEntry{db.IPv4Entries, db.IPv6Entries} {
		for i := range entries {
			if entries[i].ASN == asn {
				return entries[i].CountryCode, entries[i].ASDescription
			}
		}
	}
	return "", ""
}
