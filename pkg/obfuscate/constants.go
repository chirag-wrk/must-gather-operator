package obfuscate

const (
	// DefaultConfigPath is the built-in obfuscation config location in the operator image.
	DefaultConfigPath = "/etc/must-gather-clean/default-config.yaml"
	// WorkerCount is the number of parallel workers for must-gather-clean traversal.
	WorkerCount = 4
)
