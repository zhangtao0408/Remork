package tui

import (
	"strings"
	"testing"

	"remork/internal/output"
)

func TestActionTrackRendersSharedSymbols(t *testing.T) {
	track := ActionTrack{
		Title: "Actions",
		Actions: []ActionItem{
			{Label: "Build plan", State: ActionDone},
			{Label: "Apply changes", State: ActionRunning},
			{Label: "Refresh local state", State: ActionQueued},
			{Label: "Skipped optional sync", State: ActionSkipped},
			{Label: "Failed verify", State: ActionFailed},
		},
		SpinnerFrame: "O",
		Color:        output.ColorNever,
	}
	view := track.View()
	for _, want := range []string{
		"✓ Build plan",
		"O Apply changes",
		"· Refresh local state",
		"- Skipped optional sync",
		"× Failed verify",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestRemorkSpinnerFramesAreStable(t *testing.T) {
	want := []string{".", "o", "O", "°", "O", "o", "."}
	if got := RemorkSpinnerFrames(); strings.Join(got, "") != strings.Join(want, "") {
		t.Fatalf("frames = %#v, want %#v", got, want)
	}
}
