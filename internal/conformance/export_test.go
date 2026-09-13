package conformance

// Export for whitebox testing in conformance_test package.
var (
	SummarizeChecks          = summarizeChecks
	ExecuteCheck             = executeCheck
	WaitForExecutorHealthy   = (*Runner).waitForExecutorHealthy
	WaitForExecutorUnhealthy = (*Runner).waitForExecutorUnhealthy
	RunBattery2Handshake     = (*Runner).runBattery2Handshake
	RunBattery4Control       = (*Runner).runBattery4Control
	RunBattery6Recovery      = (*Runner).runBattery6Recovery
	RunBattery7Alerting      = (*Runner).runBattery7Alerting
)
