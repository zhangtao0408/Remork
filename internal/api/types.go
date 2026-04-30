package api

type FileType string

const (
	FileTypeFile    FileType = "file"
	FileTypeDir     FileType = "directory"
	FileTypeSymlink FileType = "symlink"
	FileTypeSpecial FileType = "special"
)

type FileEntry struct {
	Path        string   `json:"path"`
	Type        FileType `json:"type"`
	Size        int64    `json:"size"`
	ModTimeUnix int64    `json:"mtime"`
	Hash        string   `json:"hash,omitempty"`
	Revision    string   `json:"revision"`
	Large       bool     `json:"large"`
	Error       string   `json:"error,omitempty"`
}

type ManifestResponse struct {
	Root      string      `json:"root"`
	Path      string      `json:"path"`
	Revision  string      `json:"revision"`
	Entries   []FileEntry `json:"entries"`
	Threshold int64       `json:"threshold"`
}

type LargeFileMeta struct {
	Kind        string `json:"kind"`
	RemotePath  string `json:"remote_path"`
	Size        int64  `json:"size"`
	ModTimeUnix int64  `json:"mtime"`
	Hash        string `json:"hash,omitempty"`
	Revision    string `json:"revision"`
	Pulled      bool   `json:"pulled"`
	PullCommand string `json:"pull_command"`
}

type StatusResponse struct {
	Version        string   `json:"version"`
	Roots          []string `json:"roots"`
	Threshold      int64    `json:"threshold"`
	Platform       string   `json:"platform"`
	WatchSupported bool     `json:"watch_supported"`
}
