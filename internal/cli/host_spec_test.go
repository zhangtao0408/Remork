package cli

import "testing"

func TestHostConfigSpecSavesHost(t *testing.T) {
	home := t.TempDir()
	spec := HostConfigSpec{
		Name:     "lab",
		URL:      "http://127.0.0.1:17731",
		TokenEnv: "REMORK_TOKEN",
		NoProxy:  true,
	}
	if _, err := PlanHostConfig(spec); err != nil {
		t.Fatalf("PlanHostConfig: %v", err)
	}
	if err := ExecuteHostConfigSpec(Options{HomeDir: home}, spec); err != nil {
		t.Fatalf("ExecuteHostConfigSpec: %v", err)
	}
	host, ok, err := loadConfiguredHost(Options{HomeDir: home}, "lab")
	if err != nil || !ok {
		t.Fatalf("loadConfiguredHost ok=%v err=%v", ok, err)
	}
	if host.URL != spec.URL || host.TokenEnv != spec.TokenEnv || !host.NoProxy {
		t.Fatalf("host = %#v, want spec %#v", host, spec)
	}
}
