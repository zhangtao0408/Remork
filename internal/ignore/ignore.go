package ignore

import (
	"bufio"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type Matcher struct {
	patterns []pattern
}

type pattern struct {
	raw string
	dir bool
}

func Load(root string) (Matcher, error) {
	var matcher Matcher
	for _, name := range []string{".remorkignore", ".gitignore"} {
		if err := matcher.loadFile(filepath.Join(root, name)); err != nil {
			return Matcher{}, err
		}
	}
	return matcher, nil
}

func (m Matcher) Match(rel string, isDir bool) bool {
	rel = cleanRel(rel)
	if rel == "." || rel == "" {
		return false
	}
	for _, pattern := range m.patterns {
		if pattern.matches(rel, isDir) {
			return true
		}
	}
	return false
}

func (m *Matcher) loadFile(file string) error {
	f, err := os.Open(file)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(filepath.ToSlash(line), "/")
		dir := strings.HasSuffix(line, "/")
		line = strings.TrimSuffix(line, "/")
		if line == "" {
			continue
		}
		m.patterns = append(m.patterns, pattern{raw: path.Clean(line), dir: dir})
	}
	return scanner.Err()
}

func (p pattern) matches(rel string, isDir bool) bool {
	if p.dir {
		return matchDirPattern(p.raw, rel, isDir)
	}
	if hasGlob(p.raw) {
		return matchGlobPattern(p.raw, rel)
	}
	return matchExactPattern(p.raw, rel)
}

func matchDirPattern(pattern, rel string, isDir bool) bool {
	if strings.Contains(pattern, "/") {
		return (rel == pattern && isDir) || strings.HasPrefix(rel, pattern+"/")
	}
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		if part == pattern && (i < len(parts)-1 || isDir) {
			return true
		}
	}
	return false
}

func matchGlobPattern(pattern, rel string) bool {
	if strings.Contains(pattern, "/") {
		matched, err := path.Match(pattern, rel)
		return err == nil && matched
	}
	base := path.Base(rel)
	matched, err := path.Match(pattern, base)
	if err == nil && matched {
		return true
	}
	matched, err = path.Match(pattern, rel)
	return err == nil && matched
}

func matchExactPattern(pattern, rel string) bool {
	if strings.Contains(pattern, "/") {
		return rel == pattern
	}
	return rel == pattern || path.Base(rel) == pattern
}

func hasGlob(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[")
}

func cleanRel(rel string) string {
	rel = filepath.ToSlash(rel)
	rel = strings.TrimPrefix(rel, "./")
	return path.Clean(rel)
}
