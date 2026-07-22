package obfuscate

const (
	// DefaultObfuscateConfigPath is the baked-in policy path in the operator image (Phase 5).
	DefaultObfuscateConfigPath = "/etc/must-gather-clean/default-config.yaml"

	// DefaultWorkerCount is the fixed parallel worker count for must-gather-clean.
	DefaultWorkerCount = 4

	// ObfuscationLogFileName is the operator-captured audit log written beside cleaned output.
	ObfuscationLogFileName = "obfuscation.log"

	// ReportFileName is the must-gather-clean replacement mapping report written to the output directory.
	ReportFileName = "report.yaml"
)
