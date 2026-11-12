// Package version holds build metadata injected with -ldflags.
package version

var (
	// Version is the release version, for example "0.1.0". Defaults to "dev".
	Version = "dev"
	// Commit is the git commit SHA the binary was built from.
	Commit = "none"
	// Date is the build date in RFC3339 format.
	Date = "unknown"
)

// String returns a human readable version line.
func String() string {
	out := Version
	if Commit != "" && Commit != "none" {
		out += " (" + Commit + ")"
	}
	if Date != "" && Date != "unknown" {
		out += " built " + Date
	}
	return out
}
