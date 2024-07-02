package cos_agent

// Version information set by link flags during build. We fall back to these sane
// default values when we build outside the Makefile context (e.g. go build or go test).
var (
	version = "v0.0.0" // value from VERSION file
)

// GetVersion returns the version information.
func GetVersion() string {
	return version
}
