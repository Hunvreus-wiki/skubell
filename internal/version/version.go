package version

// Number is the app version string. A var (not const) so release builds can stamp it via
// -ldflags "-X github.com/Hunvreus-wiki/skubell/internal/version.Number=<tag>".
var Number = "0.1-dev"

// AppName is the user-facing application name.
const AppName = "Skubell"
