package analysis

import "time"

// timeNow is a package-level indirection over time.Now, allowing tests
// to substitute a fixed clock if needed in future Prometheus-related tests.
var timeNow = time.Now
