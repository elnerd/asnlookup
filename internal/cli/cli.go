// Package cli implements the asnlookup command-line interface. It is kept
// separate from package main so the whole flow — flag parsing, lookups,
// rendering and exit codes — can be exercised in-process by tests.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/elnerd/asnlookup/pkg/asnlookup"
)

// Process exit codes, following the usual Unix convention and Go's own flag
// package (which exits with 2 on a usage error).
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

// progName is used in the usage messages. The CLI is always invoked as
// asnlookup, so it does not depend on os.Args[0].
const progName = "asnlookup"

// defaultBenchmarkDuration is how long each timing benchmark runs.
const defaultBenchmarkDuration = 5 * time.Second

// config holds the parsed command line.
type config struct {
	cacheFile      string
	format         string
	asn            int
	timing         bool
	forceUpdate    bool
	updateInterval time.Duration
	args           []string

	// databaseURL and benchDuration are test seams. They are not part of the
	// documented interface and default to the library/CLI defaults.
	databaseURL   string
	benchDuration time.Duration
}

// usageError marks an error that should end the process with exit code 2.
type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

// Run parses args (without the program name), executes the requested
// operation and returns the process exit code. All output goes to the
// supplied writers; Run never calls os.Exit.
func Run(args []string, stdout, stderr io.Writer) int {
	cfg, err := parseFlags(args, stderr)
	if err != nil {
		// flag.ContinueOnError has already reported the problem.
		return exitUsage
	}
	return run(cfg, stdout, stderr)
}

// parseFlags turns args into a config, reporting problems on stderr.
func parseFlags(args []string, stderr io.Writer) (config, error) {
	fs := flag.NewFlagSet(progName, flag.ContinueOnError)
	fs.SetOutput(stderr)

	cacheFile := fs.String("cache-file", asnlookup.DEFAULT_CACHE_FILE, "Path to cache file")
	timing := fs.Bool("timing", false, "Run timing benchmark for 5 seconds")
	format := fs.String("format", "csv", "Output format: csv or json")
	asn := fs.Int("asn", 0, "Look up all prefixes announced by this ASN")
	forceUpdate := fs.Bool("force-update", false, "Check for a new database before serving the request")
	updateInterval := fs.Duration("update-interval", 0, "Keep the database fresh in the background at this interval (0 disables it)")
	databaseURL := fs.String("database-url", asnlookup.ASN_DATABASE_URL, "Override the upstream database URL (for testing)")
	benchDuration := fs.Duration("bench-duration", defaultBenchmarkDuration, "Duration of each timing benchmark (for testing)")

	if err := fs.Parse(args); err != nil {
		return config{}, err
	}

	return config{
		cacheFile:      *cacheFile,
		format:         *format,
		asn:            *asn,
		timing:         *timing,
		forceUpdate:    *forceUpdate,
		updateInterval: *updateInterval,
		args:           fs.Args(),
		databaseURL:    *databaseURL,
		benchDuration:  *benchDuration,
	}, nil
}

// run executes the parsed command and returns the exit code.
func run(cfg config, stdout, stderr io.Writer) int {
	// A CLI run exits immediately, so the background refresh worker is only
	// started when explicitly asked for.
	lookup, err := asnlookup.NewASNLookup(cfg.cacheFile,
		asnlookup.WithAutoUpdate(false),
		asnlookup.WithURL(cfg.databaseURL),
	)
	if err != nil {
		fmt.Fprintf(stderr, "Error loading ASN database: %v\n", err)
		return exitFailure
	}
	defer lookup.CancelScheduledUpdate()

	if cfg.forceUpdate {
		changed, err := lookup.UpdateDatabase()
		if err != nil {
			fmt.Fprintf(stderr, "Error updating ASN database: %v\n", err)
			return exitFailure
		}
		if changed {
			fmt.Fprintln(stderr, "ASN database updated")
		} else {
			fmt.Fprintln(stderr, "ASN database already up to date")
		}
	}

	if cfg.updateInterval > 0 {
		if err := lookup.ScheduleUpdateDatabase(cfg.updateInterval); err != nil {
			fmt.Fprintf(stderr, "Error scheduling ASN database updates: %v\n", err)
			return exitFailure
		}
	}

	db := lookup.Database()

	if cfg.timing {
		runTimingBenchmark(stdout, lookup, cfg.benchDuration)
		return exitOK
	}

	if cfg.asn != 0 {
		if len(cfg.args) != 0 {
			fmt.Fprintf(stderr, "Usage: %s --asn <number> [--cache-file /path/to/cache] [--format csv|json]\n", progName)
			return exitUsage
		}
		return report(stderr, printASNPrefixes(stdout, db, cfg.asn, cfg.format))
	}

	if len(cfg.args) != 1 {
		fmt.Fprintf(stderr, "Usage: %s <ip> [--cache-file /path/to/cache] [--format csv|json] [--timing]\n", progName)
		return exitUsage
	}

	ipStr := cfg.args[0]
	ip := net.ParseIP(ipStr)
	if ip == nil {
		fmt.Fprintf(stderr, "Error: invalid IP address: %s\n", ipStr)
		return exitFailure
	}

	entry := db.LookupASN(ip)
	if entry == nil {
		fmt.Fprintf(stderr, "No ASN found for IP: %s\n", ipStr)
		return exitFailure
	}

	return report(stderr, printEntry(stdout, entry, cfg.format))
}

// report writes err to stderr and maps it onto an exit code.
func report(stderr io.Writer, err error) int {
	if err == nil {
		return exitOK
	}

	fmt.Fprintln(stderr, err.Error())

	var ue usageError
	if errors.As(err, &ue) {
		return exitUsage
	}
	return exitFailure
}
