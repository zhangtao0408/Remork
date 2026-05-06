package cli

type OperationPlan struct {
	Title    string
	Target   map[string]string
	Actions  []PlannedAction
	Risks    []string
	Commands []string
	Next     []string
}

type PlannedAction struct {
	Label string
}

func (p OperationPlan) HasAction(label string) bool {
	for _, action := range p.Actions {
		if action.Label == label {
			return true
		}
	}
	return false
}

type DaemonDeploySpec struct {
	Action                          string
	HostName                        string
	SSHTarget                       string
	Roots                           []string
	Addr                            string
	LocalBin                        string
	RemoteBin                       string
	Platform                        string
	TokenFile                       string
	URL                             string
	TokenEnv                        string
	NoProxy                         bool
	Verify                          bool
	Execute                         bool
	Confirmed                       bool
	AllowUnauthenticatedNetworkBind bool
}

func BuildDaemonDeployPlan(spec DaemonDeploySpec) (OperationPlan, error) {
	deploy := daemonDeployOptions{
		action:                          spec.Action,
		hostName:                        spec.HostName,
		sshTarget:                       spec.SSHTarget,
		roots:                           append([]string(nil), spec.Roots...),
		addr:                            spec.Addr,
		localBin:                        spec.LocalBin,
		remoteBin:                       spec.RemoteBin,
		platform:                        spec.Platform,
		tokenFile:                       spec.TokenFile,
		url:                             spec.URL,
		tokenEnv:                        spec.TokenEnv,
		noProxy:                         spec.NoProxy,
		verify:                          spec.Verify,
		execute:                         spec.Execute,
		yes:                             spec.Confirmed,
		allowUnauthenticatedNetworkBind: spec.AllowUnauthenticatedNetworkBind,
	}
	applyDaemonDeployDefaults(&deploy)
	if err := validateDaemonDeployPlan(deploy); err != nil {
		return OperationPlan{}, err
	}
	if spec.Execute {
		if err := validateDaemonDeployExecution(deploy); err != nil {
			return OperationPlan{}, err
		}
	}
	remote := deploySSHTarget(deploy)
	plan := OperationPlan{
		Title: "Daemon " + deploy.action,
		Target: map[string]string{
			"host":       deploy.hostName,
			"remote":     remote,
			"remote_bin": remoteCommandPath(deploy.remoteBin),
		},
		Actions: []PlannedAction{
			{Label: "prepare remote directories"},
			{Label: "stop existing remorkd daemon"},
			{Label: "copy remorkd binary"},
			{Label: "mark remorkd executable"},
		},
		Commands: []string{
			"ssh " + shellQuote(remote) + " " + shellQuote(remotePrepareCommand(deploy)),
			"scp " + shellQuote(deploy.localBin) + " " + shellQuote(remote) + ":" + shellQuote(remoteSCPDestinationPath(deploy.remoteBin)),
			"ssh " + shellQuote(remote) + " " + shellQuote(remoteChmodCommand(deploy.remoteBin)),
		},
	}
	if remoteStartCommand(deploy) != "" {
		plan.Actions = append(plan.Actions, PlannedAction{Label: "start remorkd daemon"})
		plan.Commands = append(plan.Commands, "ssh "+shellQuote(remote)+" "+shellQuote(remoteStartCommand(deploy)))
	}
	if deploy.url != "" {
		plan.Actions = append(plan.Actions, PlannedAction{Label: "save host config"})
		plan.Next = append(plan.Next, "remork daemon status "+deploy.hostName)
	}
	if deploy.verify {
		plan.Actions = append(plan.Actions, PlannedAction{Label: "verify daemon status"})
	}
	if insecureNoTokenNonLoopbackAddr(deploy.addr, deploy.tokenFile != "") {
		plan.Risks = append(plan.Risks, "network bind without authentication")
	}
	return plan, nil
}

func applyDaemonDeployDefaults(deploy *daemonDeployOptions) {
	if deploy.action == "" {
		deploy.action = "install"
	}
	if deploy.addr == "" {
		deploy.addr = "0.0.0.0:17731"
	}
	if deploy.remoteBin == "" {
		deploy.remoteBin = ".local/bin/remorkd"
	}
}
