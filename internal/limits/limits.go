package limits

import "time"

const (
	MaxErrorBodyBytes       = 64 << 10
	MaxApplyResultBodyBytes = 8 << 20
	MaxApplyBodyBytes       = 256 << 20
	MaxExecBodyBytes        = 1 << 20
	MaxExecOutputBytes      = 8 << 20
	MaxDownloadBodyBytes    = int64(16) << 30
)

const (
	DefaultHTTPTimeout          = 30 * time.Second
	DefaultTransferTimeout      = 30 * time.Minute
	DefaultApplyTimeout         = 10 * time.Minute
	DefaultApplyBodyReadTimeout = 30 * time.Second
	DefaultExecBodyReadTimeout  = 10 * time.Second
	DefaultExecTimeout          = 10 * time.Minute
	DefaultExecWaitDelay        = 100 * time.Millisecond
	OperationTimeoutSlack       = 30 * time.Second
	DaemonReadHeaderTimeout     = 10 * time.Second
	DaemonIdleTimeout           = 2 * time.Minute
)
