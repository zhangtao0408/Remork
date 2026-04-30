package exitcode

const (
	Success              = 0
	GeneralError         = 1
	InvalidUsageOrConfig = 2
	NetworkUnavailable   = 3
	LocalDirtyBlocked    = 4
	Conflict             = 5
	PermissionDenied     = 6
	PromptRequired       = 7
	RemoteCommandFailed  = 8
	Timeout              = 9
)
