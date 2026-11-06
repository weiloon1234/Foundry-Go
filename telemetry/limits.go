package telemetry

import "time"

// Collector-specific infrastructure bounds. HTTP histogram bounds and recorder
// sample-name bounds remain owned by http and observability respectively.
const (
	maxSeries           = 4096
	maxEvents           = 4096
	maxPendingBatches   = 60
	maxRuntimeSamples   = 512
	maxSampleLabelBytes = 1024
	maxFrames           = 20
	maxIdentityBytes    = 128
	maxFunctionBytes    = 256
	maxFramePosition    = 1000000
	maxRuns             = 10000
	maxMetricRows       = 1000000
	maxIssueRows        = 10000
	maxEventRows        = 100000
	minStorageRows      = 100
	maxCaptures         = 60
	maxMinimumRequests  = 1000000
	maxMinuteRetention  = 7 * 24 * time.Hour
	maxRetention        = 30 * 24 * time.Hour
)
