// Package buildinfo holds build metadata injected via ldflags.
package buildinfo

// Build information variables, set at build time with -ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// Version returns the build version.
func Version() string { return version }

// Commit returns the git commit hash the binary was built from.
func Commit() string { return commit }

// Date returns the build date.
func Date() string { return date }

// String returns a human-readable build summary.
func String() string {
	return "version: " + version + "\ncommit: " + commit + "\ndate: " + date
}
