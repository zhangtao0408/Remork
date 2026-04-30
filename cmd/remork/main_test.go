package main

import "testing"

type fakeCodedError struct{}

func (fakeCodedError) Error() string {
	return "coded"
}

func (fakeCodedError) ExitCode() int {
	return 5
}

func TestCommandExitCodeUsesCodedError(t *testing.T) {
	if got := commandExitCode(fakeCodedError{}); got != 5 {
		t.Fatalf("exit code = %d, want 5", got)
	}
}
