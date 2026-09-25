package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRun_Table(t *testing.T) {
	cacheFile := writeCacheFile(t, fixtureTSV)

	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{
			name:       "csv IP lookup",
			args:       []string{"8.8.8.8"},
			wantCode:   0,
			wantStdout: "8.8.8.0/24, AS15169, US, \"GOOGLE\"\n",
		},
		{
			name:       "csv IPv6 lookup",
			args:       []string{"2001:4860:4860::8888"},
			wantCode:   0,
			wantStdout: "2001:4860:4860::/48, AS15169, US, \"GOOGLE\"\n",
		},
		{
			name:       "explicit csv format",
			args:       []string{"--format", "csv", "1.0.0.42"},
			wantCode:   0,
			wantStdout: "1.0.0.0/24, AS13335, US, \"CLOUDFLARENET\"\n",
		},
		{
			name:       "format is case insensitive",
			args:       []string{"--format", "CSV", "8.8.8.8"},
			wantCode:   0,
			wantStdout: "8.8.8.0/24, AS15169, US, \"GOOGLE\"\n",
		},
		{
			name:       "asn lookup csv",
			args:       []string{"--asn", "15169"},
			wantCode:   0,
			wantStdout: "8.8.8.0/24, AS15169, US, \"GOOGLE\"\n2001:4860:4860::/48, AS15169, US, \"GOOGLE\"\n",
		},
		{
			name:       "unknown format on IP lookup",
			args:       []string{"--format", "xml", "8.8.8.8"},
			wantCode:   2,
			wantStderr: "Error: unknown format 'xml'. Use 'csv' or 'json'\n",
		},
		{
			name:       "unknown format on asn lookup",
			args:       []string{"--format", "xml", "--asn", "15169"},
			wantCode:   2,
			wantStderr: "Error: unknown format 'xml'. Use 'csv' or 'json'\n",
		},
		{
			name:       "invalid IP address",
			args:       []string{"not-an-ip"},
			wantCode:   1,
			wantStderr: "Error: invalid IP address: not-an-ip\n",
		},
		{
			name:       "no ASN found",
			args:       []string{"203.0.113.1"},
			wantCode:   1,
			wantStderr: "No ASN found for IP: 203.0.113.1\n",
		},
		{
			name:       "no prefixes found",
			args:       []string{"--asn", "64512"},
			wantCode:   1,
			wantStderr: "No prefixes found for ASN: 64512\n",
		},
		{
			name:       "missing IP argument",
			args:       nil,
			wantCode:   2,
			wantStderr: "Usage: asnlookup <ip> [--cache-file /path/to/cache] [--format csv|json] [--timing]\n",
		},
		{
			name:       "too many arguments",
			args:       []string{"8.8.8.8", "1.1.1.1"},
			wantCode:   2,
			wantStderr: "Usage: asnlookup <ip> [--cache-file /path/to/cache] [--format csv|json] [--timing]\n",
		},
		{
			name:       "asn combined with a positional argument",
			args:       []string{"--asn", "15169", "8.8.8.8"},
			wantCode:   2,
			wantStderr: "Usage: asnlookup --asn <number> [--cache-file /path/to/cache] [--format csv|json]\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"--cache-file", cacheFile}, tt.args...)

			code, stdout, stderr := runCLI(t, args...)

			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d (stderr: %q)", code, tt.wantCode, stderr)
			}
			if stdout != tt.wantStdout {
				t.Errorf("stdout = %q, want %q", stdout, tt.wantStdout)
			}
			if stderr != tt.wantStderr {
				t.Errorf("stderr = %q, want %q", stderr, tt.wantStderr)
			}
		})
	}
}

func TestRun_JSONIPLookup(t *testing.T) {
	cacheFile := writeCacheFile(t, fixtureTSV)

	code, stdout, stderr := runCLI(t, "--cache-file", cacheFile, "--format", "json", "8.8.8.8")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, stderr)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}

	var got map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON (%q): %v", stdout, err)
	}

	want := map[string]interface{}{
		"net":     "8.8.8.0/24",
		"as":      float64(15169),
		"country": "us",
		"as_desc": "GOOGLE",
	}
	for key, wantValue := range want {
		if got[key] != wantValue {
			t.Errorf("%s = %v, want %v", key, got[key], wantValue)
		}
	}
}

func TestRun_JSONASNLookup(t *testing.T) {
	cacheFile := writeCacheFile(t, fixtureTSV)

	code, stdout, stderr := runCLI(t, "--cache-file", cacheFile, "--format", "json", "--asn", "15169")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, stderr)
	}

	var got struct {
		AS       int      `json:"as"`
		ASDesc   string   `json:"as_desc"`
		Country  string   `json:"country"`
		Prefixes []string `json:"prefixes"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not valid JSON (%q): %v", stdout, err)
	}

	if got.AS != 15169 {
		t.Errorf("as = %d, want 15169", got.AS)
	}
	if got.ASDesc != "GOOGLE" {
		t.Errorf("as_desc = %q, want GOOGLE", got.ASDesc)
	}
	if got.Country != "us" {
		t.Errorf("country = %q, want us", got.Country)
	}
	wantPrefixes := []string{"8.8.8.0/24", "2001:4860:4860::/48"}
	if len(got.Prefixes) != len(wantPrefixes) {
		t.Fatalf("prefixes = %v, want %v", got.Prefixes, wantPrefixes)
	}
	for i := range wantPrefixes {
		if got.Prefixes[i] != wantPrefixes[i] {
			t.Errorf("prefix %d = %s, want %s", i, got.Prefixes[i], wantPrefixes[i])
		}
	}
}

func TestRun_UnparsableFlag(t *testing.T) {
	code, stdout, stderr := runCLI(t, "--asn", "not-a-number")

	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "invalid value") {
		t.Errorf("stderr = %q, want the flag package's parse error", stderr)
	}
}

func TestRun_UnknownFlag(t *testing.T) {
	code, _, stderr := runCLI(t, "--nope")

	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "flag provided but not defined") {
		t.Errorf("stderr = %q, want an unknown-flag error", stderr)
	}
}

func TestRun_HelpRequest(t *testing.T) {
	code, _, stderr := runCLI(t, "-h")

	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "-cache-file") {
		t.Errorf("stderr = %q, want the flag usage listing", stderr)
	}
}

func TestRun_LoadError(t *testing.T) {
	// No cache file and an unreachable URL: nothing usable to serve from.
	missing := filepath.Join(t.TempDir(), "nope", "asn.cache")

	code, stdout, stderr := runCLI(t,
		"--cache-file", missing,
		"--database-url", "http://127.0.0.1:0/none",
		"8.8.8.8")

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.HasPrefix(stderr, "Error loading ASN database: ") {
		t.Errorf("stderr = %q, want the load error", stderr)
	}
}

func TestRun_ForceUpdateDownloadsDatabase(t *testing.T) {
	cacheFile := filepath.Join(t.TempDir(), "asn.cache")
	srv := newFixtureServer(t, fixtureTSVUpdated, `"v1"`)

	code, stdout, stderr := runCLI(t,
		"--cache-file", cacheFile,
		"--database-url", srv.URL,
		"--force-update",
		"9.9.9.9")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, stderr)
	}
	if stdout != "9.9.9.0/24, AS19281, US, \"QUAD9\"\n" {
		t.Errorf("stdout = %q", stdout)
	}
	// The initial load already downloaded the database, so the forced check
	// finds it unchanged.
	if stderr != "ASN database already up to date\n" {
		t.Errorf("stderr = %q, want the up-to-date notice", stderr)
	}
}

func TestRun_ForceUpdateReportsChange(t *testing.T) {
	cacheFile := writeCacheFile(t, fixtureTSV)
	srv := newFixtureServer(t, fixtureTSVUpdated, `"v2"`)

	code, stdout, stderr := runCLI(t,
		"--cache-file", cacheFile,
		"--database-url", srv.URL,
		"--force-update",
		"9.9.9.9")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, stderr)
	}
	if stderr != "ASN database updated\n" {
		t.Errorf("stderr = %q, want the updated notice", stderr)
	}
	if stdout != "9.9.9.0/24, AS19281, US, \"QUAD9\"\n" {
		t.Errorf("stdout = %q, want the entry from the refreshed database", stdout)
	}
}

func TestRun_ForceUpdateError(t *testing.T) {
	cacheFile := writeCacheFile(t, fixtureTSV)

	code, stdout, stderr := runCLI(t,
		"--cache-file", cacheFile,
		"--database-url", "http://127.0.0.1:0/none",
		"--force-update",
		"8.8.8.8")

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.HasPrefix(stderr, "Error updating ASN database: ") {
		t.Errorf("stderr = %q, want the update error", stderr)
	}
}

func TestRun_UpdateIntervalSchedulesAndStops(t *testing.T) {
	cacheFile := writeCacheFile(t, fixtureTSV)
	srv := newFixtureServer(t, fixtureTSV, `"v1"`)

	done := make(chan struct{})
	go func() {
		defer close(done)
		code, stdout, _ := runCLI(t,
			"--cache-file", cacheFile,
			"--database-url", srv.URL,
			"--update-interval", "20ms",
			"8.8.8.8")
		if code != 0 {
			t.Errorf("exit code = %d, want 0", code)
		}
		if stdout != "8.8.8.0/24, AS15169, US, \"GOOGLE\"\n" {
			t.Errorf("stdout = %q", stdout)
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return; the scheduled worker was probably not cancelled")
	}
}

func TestRun_TimingMode(t *testing.T) {
	cacheFile := writeCacheFile(t, fixtureTSV)

	code, stdout, stderr := runCLI(t,
		"--cache-file", cacheFile,
		"--timing",
		"--bench-duration", "10ms")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, stderr)
	}
	for _, want := range []string{
		"Running IP to ASN timing benchmark",
		"IP to ASN benchmark results:",
		"Running ASN to prefix timing benchmark",
		"ASN to prefix benchmark results:",
		"Prefixes/lookup:",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout is missing %q:\n%s", want, stdout)
		}
	}
}

func TestParseFlags_Defaults(t *testing.T) {
	cfg, err := parseFlags(nil, nil)
	if err != nil {
		t.Fatalf("parseFlags() error = %v", err)
	}

	if cfg.format != "csv" {
		t.Errorf("format = %q, want csv", cfg.format)
	}
	if cfg.asn != 0 {
		t.Errorf("asn = %d, want 0", cfg.asn)
	}
	if cfg.timing || cfg.forceUpdate {
		t.Error("timing/force-update default to true, want false")
	}
	if cfg.updateInterval != 0 {
		t.Errorf("updateInterval = %s, want 0", cfg.updateInterval)
	}
	if cfg.benchDuration != defaultBenchmarkDuration {
		t.Errorf("benchDuration = %s, want %s", cfg.benchDuration, defaultBenchmarkDuration)
	}
	if len(cfg.args) != 0 {
		t.Errorf("args = %v, want empty", cfg.args)
	}
}

func TestParseFlags_AllValues(t *testing.T) {
	cfg, err := parseFlags([]string{
		"--cache-file", "/tmp/x.cache",
		"--format", "json",
		"--asn", "15169",
		"--timing",
		"--force-update",
		"--update-interval", "2h",
		"--database-url", "http://example.invalid/db",
		"--bench-duration", "250ms",
		"8.8.8.8",
	}, nil)
	if err != nil {
		t.Fatalf("parseFlags() error = %v", err)
	}

	if cfg.cacheFile != "/tmp/x.cache" {
		t.Errorf("cacheFile = %q", cfg.cacheFile)
	}
	if cfg.format != "json" {
		t.Errorf("format = %q", cfg.format)
	}
	if cfg.asn != 15169 {
		t.Errorf("asn = %d", cfg.asn)
	}
	if !cfg.timing || !cfg.forceUpdate {
		t.Error("timing/force-update were not set")
	}
	if cfg.updateInterval != 2*time.Hour {
		t.Errorf("updateInterval = %s", cfg.updateInterval)
	}
	if cfg.databaseURL != "http://example.invalid/db" {
		t.Errorf("databaseURL = %q", cfg.databaseURL)
	}
	if cfg.benchDuration != 250*time.Millisecond {
		t.Errorf("benchDuration = %s", cfg.benchDuration)
	}
	if len(cfg.args) != 1 || cfg.args[0] != "8.8.8.8" {
		t.Errorf("args = %v, want [8.8.8.8]", cfg.args)
	}
}
