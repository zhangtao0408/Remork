package diff

import (
	"strings"
	"testing"
)

func TestUnifiedDiffShowsRemovedAndAddedLines(t *testing.T) {
	got := UnifiedText("a.txt", []byte("one\n"), []byte("two\n"))
	for _, want := range []string{"--- a.txt", "+++ a.txt", "-one", "+two"} {
		if !strings.Contains(got, want) {
			t.Fatalf("UnifiedText output missing %q:\n%s", want, got)
		}
	}
}

func TestMetadataDiffForBinary(t *testing.T) {
	got := Metadata("model.bin", MetadataChange{OldSize: 10, NewSize: 12, Large: true})
	for _, want := range []string{"model.bin", "binary or large file", "10 -> 12 bytes"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Metadata output missing %q:\n%s", want, got)
		}
	}
}
