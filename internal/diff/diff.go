package diff

import (
	"fmt"
	"strings"
)

type MetadataChange struct {
	OldSize int64
	NewSize int64
	Large   bool
}

func UnifiedText(path string, oldData, newData []byte) string {
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n", path)
	fmt.Fprintf(&b, "+++ %s\n", path)
	oldLines := splitLines(string(oldData))
	newLines := splitLines(string(newData))
	commonPrefix := 0
	for commonPrefix < len(oldLines) && commonPrefix < len(newLines) && oldLines[commonPrefix] == newLines[commonPrefix] {
		commonPrefix++
	}
	commonSuffix := 0
	for commonSuffix < len(oldLines)-commonPrefix && commonSuffix < len(newLines)-commonPrefix &&
		oldLines[len(oldLines)-1-commonSuffix] == newLines[len(newLines)-1-commonSuffix] {
		commonSuffix++
	}
	for _, line := range oldLines[commonPrefix : len(oldLines)-commonSuffix] {
		fmt.Fprintf(&b, "-%s\n", line)
	}
	for _, line := range newLines[commonPrefix : len(newLines)-commonSuffix] {
		fmt.Fprintf(&b, "+%s\n", line)
	}
	return b.String()
}

func Metadata(path string, change MetadataChange) string {
	kind := "binary file"
	if change.Large {
		kind = "binary or large file"
	}
	return fmt.Sprintf("%s\n%s\n%d -> %d bytes\n", path, kind, change.OldSize, change.NewSize)
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
