package remoteroot

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type Root struct {
	Raw   string
	Clean string
}

func Normalize(root string) (Root, error) {
	if root == "" {
		return Root{}, fmt.Errorf("root is required")
	}
	if !isRemoteAbs(root) {
		return Root{}, fmt.Errorf("root %q must be absolute", root)
	}
	return Root{Raw: root, Clean: cleanRemote(root)}, nil
}

func NormalizeMany(roots []string) ([]Root, error) {
	out := make([]Root, 0, len(roots))
	for _, root := range roots {
		normalized, err := Normalize(root)
		if err != nil {
			return nil, err
		}
		out = append(out, normalized)
	}
	return out, nil
}

func Contains(allowed []Root, candidate string) (bool, error) {
	requested, err := Normalize(candidate)
	if err != nil {
		return false, err
	}
	return containsClean(allowed, requested.Clean)
}

func ContainsResolved(allowed []Root, candidate string) (bool, error) {
	_, ok, err := ResolveAllowed(allowed, candidate)
	return ok, err
}

func ResolveAllowed(allowed []Root, candidate string) (string, bool, error) {
	if candidate == "" {
		return "", false, fmt.Errorf("root is required")
	}
	if !isRemoteAbs(candidate) {
		return "", false, fmt.Errorf("root %q must be absolute", candidate)
	}
	requestedReal, err := evalSymlinksPreservingTraversal(candidate, 255)
	if err != nil {
		return "", false, err
	}
	realAllowed := make([]Root, 0, len(allowed))
	for _, base := range allowed {
		if err := validateAllowedRoot(base); err != nil {
			return "", false, err
		}
		basePath := base.Raw
		if basePath == "" {
			basePath = base.Clean
		}
		if !isRemoteAbs(basePath) {
			return "", false, fmt.Errorf("allowed root %q must be absolute", basePath)
		}
		baseReal, err := evalSymlinksPreservingTraversal(basePath, 255)
		if err != nil {
			return "", false, err
		}
		realAllowed = append(realAllowed, Root{Raw: base.Raw, Clean: cleanRemote(baseReal)})
	}
	canonical := cleanRemote(requestedReal)
	ok, err := containsClean(realAllowed, canonical)
	return canonical, ok, err
}

func ResolveWorkspacePath(allowed []Root, selectedRoot string, input string) (string, error) {
	input = strings.TrimSpace(input)
	if strings.HasPrefix(input, "~") {
		return "", fmt.Errorf("workspace path %q is not expanded by remork connect; use an absolute remote path such as /home/me/project", input)
	}
	if input == "" {
		base, err := Normalize(selectedRoot)
		if err != nil {
			return "", err
		}
		ok, err := containsClean(allowed, base.Clean)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("workspace path %q is outside advertised allowed roots", base.Clean)
		}
		return base.Clean, nil
	}
	if isRemoteAbs(input) {
		candidate, err := Normalize(input)
		if err != nil {
			return "", err
		}
		ok, err := containsClean(allowed, candidate.Clean)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("workspace path %q is outside advertised allowed roots", candidate.Clean)
		}
		return candidate.Clean, nil
	}
	base, err := Normalize(selectedRoot)
	if err != nil {
		return "", err
	}
	candidate := cleanRemote(path.Join(base.Clean, input))
	ok, err := containsClean(allowed, candidate)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("workspace path %q is outside advertised allowed roots", candidate)
	}
	return candidate, nil
}

func containsClean(allowed []Root, requestedClean string) (bool, error) {
	for _, base := range allowed {
		if err := validateAllowedRoot(base); err != nil {
			return false, err
		}
		if requestedClean == base.Clean {
			return true, nil
		}
		prefix := strings.TrimRight(base.Clean, "/") + "/"
		if strings.HasPrefix(requestedClean, prefix) {
			return true, nil
		}
	}
	return false, nil
}

func validateAllowedRoot(root Root) error {
	if root.Clean == "" {
		return fmt.Errorf("allowed root is empty")
	}
	if !isRemoteAbs(root.Clean) {
		return fmt.Errorf("allowed root %q must be absolute", root.Clean)
	}
	if cleaned := cleanRemote(root.Clean); cleaned != root.Clean {
		return fmt.Errorf("allowed root %q is not clean", root.Clean)
	}
	return nil
}

func isRemoteAbs(root string) bool {
	return path.IsAbs(root)
}

func cleanRemote(root string) string {
	return path.Clean(root)
}

func evalSymlinksPreservingTraversal(path string, linksRemaining int) (string, error) {
	if linksRemaining <= 0 {
		return "", fmt.Errorf("too many symlinks while resolving %q", path)
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("root %q must be absolute", path)
	}
	current := string(filepath.Separator)
	for i, part := range strings.Split(path, string(filepath.Separator)) {
		if i == 0 || part == "" || part == "." {
			continue
		}
		if part == ".." {
			current = filepath.Dir(current)
			continue
		}
		next := filepath.Join(current, part)
		info, err := os.Lstat(next)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			current = next
			continue
		}
		target, err := os.Readlink(next)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(target) {
			target = current + string(filepath.Separator) + target
		}
		remaining := strings.Join(strings.Split(path, string(filepath.Separator))[i+1:], string(filepath.Separator))
		if remaining != "" {
			target += string(filepath.Separator) + remaining
		}
		return evalSymlinksPreservingTraversal(target, linksRemaining-1)
	}
	return filepath.Clean(current), nil
}
