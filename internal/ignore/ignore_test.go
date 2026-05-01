package ignore

import "testing"

func TestDirectoryOnlyPatternDoesNotMatchRegularFileWithSameName(t *testing.T) {
	matcher := Matcher{patterns: []pattern{{raw: "cache", dir: true}}}

	tests := []struct {
		name string
		rel  string
		dir  bool
		want bool
	}{
		{name: "directory itself", rel: "cache", dir: true, want: true},
		{name: "descendant at root", rel: "cache/file.txt", dir: false, want: true},
		{name: "descendant below directory component", rel: "src/cache/file.txt", dir: false, want: true},
		{name: "regular file with same name", rel: "cache", dir: false, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matcher.Match(tt.rel, tt.dir); got != tt.want {
				t.Fatalf("Match(%q, %v) = %v, want %v", tt.rel, tt.dir, got, tt.want)
			}
		})
	}
}

func TestDirectoryOnlyPatternWithSlashDoesNotMatchRegularFileAtPatternPath(t *testing.T) {
	matcher := Matcher{patterns: []pattern{{raw: "build/cache", dir: true}}}

	tests := []struct {
		name string
		rel  string
		dir  bool
		want bool
	}{
		{name: "directory itself", rel: "build/cache", dir: true, want: true},
		{name: "descendant", rel: "build/cache/file.txt", dir: false, want: true},
		{name: "regular file at pattern path", rel: "build/cache", dir: false, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matcher.Match(tt.rel, tt.dir); got != tt.want {
				t.Fatalf("Match(%q, %v) = %v, want %v", tt.rel, tt.dir, got, tt.want)
			}
		})
	}
}
