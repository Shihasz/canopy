package rollout

// VM represents one target host the controller can deploy to.
type VM struct {
	Name string // logical name, e.g. "canary-1"
	Host string // SSH-reachable address, e.g. "10.0.0.5" or "canary-1.internal"
	Port int    // SSH port, typically 22
	User string // SSH user
	Role Role
}

// Role distinguishes canary hosts (receiving the new version first)
// from stable hosts (running the last known-good version).
type Role string

const (
	RoleCanary Role = "canary"
	RoleStable Role = "stable"
)
