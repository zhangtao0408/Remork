package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"remork/internal/api"
)

const defaultLargeThreshold = 128 << 20

type Options struct {
	LargeThreshold int64
}

func Scan(root string, path string, opts Options) (api.ManifestResponse, error) {
	if opts.LargeThreshold <= 0 {
		opts.LargeThreshold = defaultLargeThreshold
	}
	start := filepath.Join(root, filepath.FromSlash(path))
	var entries []api.FileEntry
	err := filepath.WalkDir(start, func(p string, d os.DirEntry, walkErr error) error {
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if walkErr != nil {
			entries = append(entries, api.FileEntry{Path: rel, Error: walkErr.Error()})
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == ".remork") {
			return filepath.SkipDir
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			entries = append(entries, api.FileEntry{Path: rel, Error: infoErr.Error()})
			return nil
		}
		entry := api.FileEntry{
			Path:        rel,
			Size:        info.Size(),
			ModTimeUnix: info.ModTime().Unix(),
			Revision:    revisionFor(info),
		}
		switch {
		case d.Type().IsRegular():
			entry.Type = api.FileTypeFile
			entry.Large = info.Size() > opts.LargeThreshold
			if !entry.Large {
				hash, err := HashFile(p)
				if err != nil {
					entry.Error = err.Error()
				} else {
					entry.Hash = hash
				}
			}
		case d.IsDir():
			entry.Type = api.FileTypeDir
		case d.Type()&os.ModeSymlink != 0:
			entry.Type = api.FileTypeSymlink
		default:
			entry.Type = api.FileTypeSpecial
		}
		entries = append(entries, entry)
		return nil
	})
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return api.ManifestResponse{
		Root:      root,
		Path:      path,
		Revision:  manifestRevision(entries),
		Entries:   entries,
		Threshold: opts.LargeThreshold,
	}, err
}

func BuildLargeMeta(workspaceRef string, entry api.FileEntry) api.LargeFileMeta {
	return api.LargeFileMeta{
		Kind:        "remote-large-file",
		RemotePath:  "/" + strings.TrimPrefix(entry.Path, "/"),
		Size:        entry.Size,
		ModTimeUnix: entry.ModTimeUnix,
		Hash:        entry.Hash,
		Revision:    entry.Revision,
		Pulled:      false,
		PullCommand: "remork pull " + strings.TrimRight(workspaceRef, "/") + "/" + entry.Path,
	}
}

func EntryForTest(path string, size int64, large bool) api.FileEntry {
	return api.FileEntry{Path: path, Size: size, Large: large, Revision: "rev-test"}
}

func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func revisionFor(info os.FileInfo) string {
	return info.ModTime().UTC().Format("20060102150405") + "-" + strconv.FormatInt(info.Size(), 10)
}

func manifestRevision(entries []api.FileEntry) string {
	h := sha256.New()
	for _, e := range entries {
		io.WriteString(h, e.Path)
		io.WriteString(h, string(e.Type))
		io.WriteString(h, e.Revision)
		io.WriteString(h, e.Hash)
		io.WriteString(h, strconv.FormatBool(e.Large))
		io.WriteString(h, e.Error)
	}
	return "rev-" + hex.EncodeToString(h.Sum(nil))[:16]
}
