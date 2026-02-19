package version

// Version is set at build time via -ldflags "-X github.com/eargollo/ditto/internal/version.Version=..."
// e.g. go build -ldflags "-X github.com/eargollo/ditto/internal/version.Version=v1.0.0" ./cmd/ditto
var Version string
