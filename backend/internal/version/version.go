// Package version holds the core platform's own version — separate from
// any module's version (see modules/*/manifest.yaml's own version field),
// and from the module registry's index (see registryclient.IndexEntry's
// MinPlatformVersion, which compares against this to decide whether a
// module update needs a newer platform first).
package version

// Version is a plain var, not a const, so a real release build can
// override it at compile time (-ldflags "-X
// it-platform/backend/internal/version.Version=1.2.3") once there's an
// actual CI pipeline cutting tagged builds — until then this literal is
// the source of truth, bumped by hand alongside module versions.
var Version = "1.0.0"
