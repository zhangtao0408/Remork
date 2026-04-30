package preflight

import "remork/internal/exitcode"

type WorkspaceState struct {
	LocalDirty  int
	RemoteStale bool
	Conflicts   int
}

type Options struct {
	RemoteOnly  bool
	NoSyncCheck bool
}

type Decision struct {
	Allow    bool
	ExitCode int
	Message  string
	Warning  string
}

func Decide(state WorkspaceState, opts Options) Decision {
	if opts.RemoteOnly {
		return Decision{
			Allow:   true,
			Warning: "warning: local pending changes are ignored by --remote-only",
		}
	}
	if opts.NoSyncCheck {
		return Decision{Allow: true}
	}
	if state.Conflicts > 0 {
		return Decision{
			ExitCode: exitcode.Conflict,
			Message:  "Conflicts exist; resolve conflicts before running remote commands.",
		}
	}
	if state.LocalDirty > 0 {
		return Decision{
			ExitCode: exitcode.LocalDirtyBlocked,
			Message:  "Local changes exist; run remork apply first or use --remote-only to ignore local pending changes.",
		}
	}
	return Decision{Allow: true}
}
