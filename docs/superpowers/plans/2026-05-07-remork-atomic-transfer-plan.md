# Remork Atomic Transfer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make sync and pull never expose partially downloaded local files, and verify downloaded bytes before replacing the final file.

**Architecture:** Keep the existing same-directory atomic write design in `internal/transfer`. Add a transfer-specific temp naming convention, optional post-write verification, stale temp cleanup, and tests around sync/pull behavior. Do not expose user-visible `file.py.remork`; use hidden temp files like `.file.py.remork-*` and atomically rename after successful download and verification.

**Tech Stack:** Go, `os.CreateTemp`, `os.Rename`, existing `internal/transfer`, `internal/syncer`, `internal/state`, `go test`.

---

## File Structure

- Modify `internal/transfer/transfer.go`
  - Own local path safety and atomic file replacement.
  - Add explicit temp suffix helper and optional verification hook.
  - Add stale temp cleanup helper scoped to one target file.
- Modify `internal/transfer/transfer_test.go`
  - Cover incomplete writes, hidden temp naming, stale temp cleanup, and hash verification behavior.
- Modify `internal/syncer/syncer.go`
  - Use the transfer verification hook after `DownloadToContext`.
  - Clean stale transfer temps before each file replacement.
- Modify `internal/syncer/syncer_test.go`
  - Cover interrupted download preserving existing local file.
  - Cover hash mismatch preserving existing local file and returning an actionable error.

---

### Task 1: Make Atomic Transfer Semantics Explicit

**Files:**
- Modify: `internal/transfer/transfer.go`
- Test: `internal/transfer/transfer_test.go`

- [ ] **Step 1: Write failing tests for incomplete writes and temp naming**

Add these tests to `internal/transfer/transfer_test.go`:

```go
func TestWriteFileWithKeepsExistingFileWhenWriterFails(t *testing.T) {
	local := t.TempDir()
	target := filepath.Join(local, "src", "file_a.py")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old complete file\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := WriteFileWith(local, "src/file_a.py", func(w io.Writer) error {
		if _, writeErr := w.Write([]byte("partial new file")); writeErr != nil {
			return writeErr
		}
		return errors.New("simulated transfer failure")
	})
	if err == nil {
		t.Fatal("WriteFileWith error = nil, want simulated failure")
	}

	got, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "old complete file\n" {
		t.Fatalf("target content = %q, want old complete file", got)
	}

	matches, globErr := filepath.Glob(filepath.Join(local, "src", ".file_a.py.remork-*"))
	if globErr != nil {
		t.Fatal(globErr)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary transfer files left behind: %#v", matches)
	}
}

func TestWriteFileWithUsesHiddenRemorkTempFile(t *testing.T) {
	local := t.TempDir()
	var tempName string

	err := WriteFileWith(local, "src/file_a.py", func(w io.Writer) error {
		if named, ok := w.(interface{ Name() string }); ok {
			tempName = filepath.Base(named.Name())
		}
		_, writeErr := w.Write([]byte("complete\n"))
		return writeErr
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tempName, ".file_a.py.remork-") {
		t.Fatalf("temp name = %q, want .file_a.py.remork-*", tempName)
	}
}
```

- [ ] **Step 2: Run tests to verify current behavior**

Run:

```bash
go test ./internal/transfer -run 'TestWriteFileWithKeepsExistingFileWhenWriterFails|TestWriteFileWithUsesHiddenRemorkTempFile' -count=1
```

Expected:
- The incomplete write test should pass if current cleanup is already correct.
- The temp naming test should pass only if `WriteFileWith` exposes a named temp writer and uses `.file.remork-*`.

- [ ] **Step 3: Make the temp writer contract explicit**

If the temp naming test cannot observe the temp file name, introduce this local interface and wrapper in `internal/transfer/transfer.go`:

```go
type atomicTempFile struct {
	*os.File
}
```

Then change the writer call inside `writeFileAtomic`:

```go
if err := write(atomicTempFile{File: tmpFile}); err != nil {
	_ = tmpFile.Close()
	return err
}
```

Keep temp creation in the same directory:

```go
tmpFile, err := os.CreateTemp(parent, "."+filepath.Base(full)+".remork-*")
```

- [ ] **Step 4: Run transfer tests**

Run:

```bash
go test ./internal/transfer -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/transfer/transfer.go internal/transfer/transfer_test.go
git commit -m "test: document atomic transfer temp semantics"
```

---

### Task 2: Add Download Verification Before Rename

**Files:**
- Modify: `internal/transfer/transfer.go`
- Modify: `internal/syncer/syncer.go`
- Test: `internal/transfer/transfer_test.go`
- Test: `internal/syncer/syncer_test.go`

- [ ] **Step 1: Add transfer API with post-write verification**

Add this type and function to `internal/transfer/transfer.go`:

```go
type WriteOptions struct {
	Verify func(path string) error
}

func WriteFileWithOptions(localRoot, remotePath string, opts WriteOptions, write func(io.Writer) error) error {
	full, err := LocalPath(localRoot, remotePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return writeFileAtomicWithOptions(full, opts, write)
}
```

Replace `writeFileAtomic` internals with a shared implementation:

```go
func writeFileAtomic(full string, write func(io.Writer) error) error {
	return writeFileAtomicWithOptions(full, WriteOptions{}, write)
}

func writeFileAtomicWithOptions(full string, opts WriteOptions, write func(io.Writer) error) error {
	parent := filepath.Dir(full)
	tmpFile, err := os.CreateTemp(parent, "."+filepath.Base(full)+".remork-*")
	if err != nil {
		return err
	}
	tmp := tmpFile.Name()
	keepTemp := false
	defer func() {
		if !keepTemp {
			_ = os.Remove(tmp)
		}
	}()
	if err := write(atomicTempFile{File: tmpFile}); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Chmod(0o644); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	if opts.Verify != nil {
		if err := opts.Verify(tmp); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, full); err != nil {
		return err
	}
	keepTemp = true
	return nil
}
```

- [ ] **Step 2: Write failing transfer verification test**

Add to `internal/transfer/transfer_test.go`:

```go
func TestWriteFileWithOptionsRejectsFailedVerificationBeforeRename(t *testing.T) {
	local := t.TempDir()
	target := filepath.Join(local, "file_a.py")
	if err := os.WriteFile(target, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := WriteFileWithOptions(local, "file_a.py", WriteOptions{
		Verify: func(path string) error {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if string(data) != "expected\n" {
				return fmt.Errorf("download verification failed")
			}
			return nil
		},
	}, func(w io.Writer) error {
		_, writeErr := w.Write([]byte("corrupt\n"))
		return writeErr
	})
	if err == nil || !strings.Contains(err.Error(), "download verification failed") {
		t.Fatalf("error = %v, want download verification failure", err)
	}

	got, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "old\n" {
		t.Fatalf("target content = %q, want old content preserved", got)
	}
}
```

- [ ] **Step 3: Wire syncer hash verification**

Modify `downloadFile` in `internal/syncer/syncer.go`:

```go
expectedHash := op.Entry.Hash
if err := transfer.WriteFileWithOptions(r.opts.LocalRoot, op.Path, transfer.WriteOptions{
	Verify: func(path string) error {
		if expectedHash == "" {
			return nil
		}
		got, hashErr := state.HashFile(path)
		if hashErr != nil {
			return hashErr
		}
		if got != expectedHash {
			return fmt.Errorf("download verification failed for %s: hash %s, want %s", op.Path, got, expectedHash)
		}
		return nil
	},
}, func(w io.Writer) error {
	_, err := r.opts.Client.DownloadToContext(ctx, r.opts.RemoteRoot, op.Path, w)
	return err
}); err != nil {
	return err
}
```

Leave the existing fallback hash computation after download:

```go
hash := op.Entry.Hash
if hash == "" {
	computedHash, err := state.HashFile(localPath)
	if err != nil {
		return err
	}
	hash = computedHash
}
```

- [ ] **Step 4: Add syncer regression test for hash mismatch**

Add to `internal/syncer/syncer_test.go`:

```go
func TestSyncHashMismatchPreservesExistingLocalFile(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(remote, "file_a.py"), []byte("old\n"))
	store := state.NewStore(filepath.Join(local, ".remork", "state"))
	runner := Runner{
		opts: RunnerOptions{
			Client:     client.NewLocal(remote),
			StateStore: store,
			LocalRoot:  local,
			RemoteRoot: remote,
		},
	}
	if _, err := runner.Sync(context.Background(), SyncOptions{}); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	mustWriteFile(t, filepath.Join(remote, "file_a.py"), []byte("new\n"))
	snap, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	tracked := snap.Entries["file_a.py"]
	tracked.BaseHash = "definitely-not-the-real-hash"
	snap.Entries["file_a.py"] = tracked
	if err := store.Save(snap); err != nil {
		t.Fatal(err)
	}

	_, err = runner.Sync(context.Background(), SyncOptions{Force: true})
	if err == nil || !strings.Contains(err.Error(), "download verification failed") {
		t.Fatalf("sync error = %v, want verification failure", err)
	}
	got, readErr := os.ReadFile(filepath.Join(local, "file_a.py"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "old\n" {
		t.Fatalf("local file = %q, want old file preserved", got)
	}
}
```

- [ ] **Step 5: Run focused tests**

Run:

```bash
go test ./internal/transfer -count=1
go test ./internal/syncer -run 'TestSyncHashMismatchPreservesExistingLocalFile' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/transfer/transfer.go internal/transfer/transfer_test.go internal/syncer/syncer.go internal/syncer/syncer_test.go
git commit -m "fix: verify downloads before replacing local files"
```

---

### Task 3: Clean Stale Transfer Temp Files

**Files:**
- Modify: `internal/transfer/transfer.go`
- Modify: `internal/syncer/syncer.go`
- Test: `internal/transfer/transfer_test.go`
- Test: `internal/syncer/syncer_test.go`

- [ ] **Step 1: Add cleanup helper**

Add to `internal/transfer/transfer.go`:

```go
func CleanupStaleTemps(localRoot, remotePath string) error {
	full, err := LocalPath(localRoot, remotePath)
	if err != nil {
		return err
	}
	pattern := filepath.Join(filepath.Dir(full), "."+filepath.Base(full)+".remork-*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	for _, match := range matches {
		if err := os.Remove(match); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 2: Add cleanup test**

Add to `internal/transfer/transfer_test.go`:

```go
func TestCleanupStaleTempsOnlyRemovesTargetTemps(t *testing.T) {
	local := t.TempDir()
	dir := filepath.Join(local, "src")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, ".file_a.py.remork-old")
	other := filepath.Join(dir, ".file_b.py.remork-old")
	if err := os.WriteFile(stale, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("other"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := CleanupStaleTemps(local, "src/file_a.py"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale temp still exists or stat failed: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("other temp should remain: %v", err)
	}
}
```

- [ ] **Step 3: Call cleanup before file replacement**

Modify `downloadFile` in `internal/syncer/syncer.go` immediately after `prepareFileReplacement`:

```go
if err := transfer.CleanupStaleTemps(r.opts.LocalRoot, op.Path); err != nil {
	return err
}
```

- [ ] **Step 4: Add syncer test for stale cleanup**

Add to `internal/syncer/syncer_test.go`:

```go
func TestSyncRemovesStaleTransferTempBeforeDownload(t *testing.T) {
	remote := t.TempDir()
	local := t.TempDir()
	mustWriteFile(t, filepath.Join(remote, "file_a.py"), []byte("remote\n"))
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(local, ".file_a.py.remork-old")
	if err := os.WriteFile(stale, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := Runner{
		opts: RunnerOptions{
			Client:     client.NewLocal(remote),
			StateStore: state.NewStore(filepath.Join(local, ".remork", "state")),
			LocalRoot:  local,
			RemoteRoot: remote,
		},
	}
	if _, err := runner.Sync(context.Background(), SyncOptions{}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale temp still exists or stat failed: %v", err)
	}
}
```

- [ ] **Step 5: Run focused tests**

Run:

```bash
go test ./internal/transfer -count=1
go test ./internal/syncer -run 'TestSyncRemovesStaleTransferTempBeforeDownload' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/transfer/transfer.go internal/transfer/transfer_test.go internal/syncer/syncer.go internal/syncer/syncer_test.go
git commit -m "fix: clean stale transfer temp files"
```

---

### Task 4: Validate Packaging and Release Readiness

**Files:**
- Modify only if failures are found.

- [ ] **Step 1: Run focused package tests**

Run:

```bash
go test ./internal/transfer ./internal/syncer ./internal/remoteroot -count=1
```

Expected: PASS.

- [ ] **Step 2: Run broader tests when local loopback allows it**

Run:

```bash
go test ./...
```

Expected: PASS. If local `127.0.0.1` httptest failures appear with `connect: can't assign requested address`, record that as the known local loopback environment issue and use GitHub Actions as the full validation gate.

- [ ] **Step 3: Build release and npm package**

Run:

```bash
bash scripts/build-release.sh v0.1.1-beta.8
bash scripts/build-npm-package.sh v0.1.1-beta.8
bash scripts/smoke-npm-package.sh
```

Expected:
- Release binaries are generated in `dist/`.
- npm package dry-run names `@zhangtao0408/remork@0.1.1-beta.8`.
- smoke install prints `remork 0.1.1-beta.8`.

- [ ] **Step 4: Commit any validation-only fixes**

If Step 1-3 required code changes:

```bash
git add <changed files>
git commit -m "fix: harden atomic transfer release validation"
```

If no files changed, skip this commit.

---

## Self-Review

Spec coverage:
- Partial download should not expose incomplete final files: Task 1 and Task 2.
- `.remork` transfer suffix direction: Task 1 keeps hidden `.file.remork-*` temp files rather than visible `file.remork`.
- Rename after successful transfer: Task 1 uses existing same-directory `os.Rename`.
- Stronger safety than suffix-only flow: Task 2 adds hash verification before rename.
- Stale interrupted temp cleanup: Task 3.
- Windows packaging confidence: Task 4 runs existing npm smoke and release build.

Placeholder scan:
- No task uses TBD/TODO.
- Each code-changing step includes concrete code.
- Each test step includes exact commands and expected result.

Type consistency:
- `transfer.WriteOptions` is defined before `WriteFileWithOptions` uses it.
- `CleanupStaleTemps` is defined before syncer calls it.
- `state.HashFile` already exists and is used elsewhere in `internal/syncer/syncer.go`.

