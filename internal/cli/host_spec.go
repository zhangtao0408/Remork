package cli

import (
	"fmt"

	"remork/internal/config"
)

type HostConfigSpec struct {
	Name      string
	URL       string
	TokenEnv  string
	TokenFile string
	NoProxy   bool
}

func PlanHostConfig(spec HostConfigSpec) (OperationPlan, error) {
	if spec.Name == "" {
		return OperationPlan{}, fmt.Errorf("host name is required")
	}
	if spec.URL == "" {
		return OperationPlan{}, fmt.Errorf("--url is required")
	}
	if err := validateDaemonURL(spec.URL); err != nil {
		return OperationPlan{}, err
	}
	return OperationPlan{
		Title: "Save host",
		Target: map[string]string{
			"name": spec.Name,
			"url":  spec.URL,
		},
		Actions: []PlannedAction{{Label: "save host config"}},
		Next:    []string{"remork daemon status " + spec.Name},
	}, nil
}

func ExecuteHostConfigSpec(opts Options, spec HostConfigSpec) error {
	if _, err := PlanHostConfig(spec); err != nil {
		return err
	}
	store, err := configStore(opts)
	if err != nil {
		return err
	}
	cfg, err := store.LoadOrDefault()
	if err != nil {
		return err
	}
	cfg.Hosts[spec.Name] = config.Host{Name: spec.Name, URL: spec.URL, TokenEnv: spec.TokenEnv, TokenFile: spec.TokenFile, NoProxy: spec.NoProxy}
	return store.Save(cfg)
}
