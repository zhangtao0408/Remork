package cli

import "testing"

func TestLimitedPathsLimitsNonVerboseOutput(t *testing.T) {
	paths := []string{"p00", "p01", "p02", "p03", "p04", "p05", "p06", "p07", "p08", "p09", "p10"}

	got, more := limitedPaths(paths, false)
	if len(got) != 10 {
		t.Fatalf("limited paths len = %d, want 10", len(got))
	}
	if more != 1 {
		t.Fatalf("more = %d, want 1", more)
	}
	if got[9] != "p09" {
		t.Fatalf("last limited path = %q, want p09", got[9])
	}
}

func TestLimitedPathsReturnsAllWhenVerbose(t *testing.T) {
	paths := []string{"p00", "p01", "p02", "p03", "p04", "p05", "p06", "p07", "p08", "p09", "p10"}

	got, more := limitedPaths(paths, true)
	if len(got) != len(paths) {
		t.Fatalf("verbose paths len = %d, want %d", len(got), len(paths))
	}
	if more != 0 {
		t.Fatalf("more = %d, want 0", more)
	}
}

func TestShellQuotePath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "simple path remains unquoted",
			path: "a.txt",
			want: "a.txt",
		},
		{
			name: "path with spaces is single quoted",
			path: "two words.txt",
			want: "'two words.txt'",
		},
		{
			name: "path with command substitution is single quoted",
			path: "x/$(touch pwned).txt",
			want: "'x/$(touch pwned).txt'",
		},
		{
			name: "path with single quote is safely escaped",
			path: "owner's notes.txt",
			want: "'owner'\"'\"'s notes.txt'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shellQuotePath(tt.path); got != tt.want {
				t.Fatalf("shellQuotePath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestPathCommandUsesFlagTerminatorAndShellQuote(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		path string
		want string
	}{
		{
			name: "dash prefixed path",
			cmd:  "diff",
			path: "-dash.txt",
			want: "remork diff -- -dash.txt",
		},
		{
			name: "path with spaces",
			cmd:  "restore",
			path: "two words.txt",
			want: "remork restore -- 'two words.txt'",
		},
		{
			name: "path with command substitution",
			cmd:  "conflict",
			path: "x/$(touch pwned).txt",
			want: "remork conflict -- 'x/$(touch pwned).txt'",
		},
		{
			name: "path with single quote",
			cmd:  "conflict",
			path: "owner's notes.txt",
			want: "remork conflict -- 'owner'\"'\"'s notes.txt'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pathCommand(tt.cmd, tt.path); got != tt.want {
				t.Fatalf("pathCommand(%q, %q) = %q, want %q", tt.cmd, tt.path, got, tt.want)
			}
		})
	}
}
