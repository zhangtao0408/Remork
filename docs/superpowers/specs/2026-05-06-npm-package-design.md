# Remork npm Package Design

Date: 2026-05-06

## Goal

Publish Remork as an npm-installable CLI so users can run:

```bash
npm install -g remork
remork setup
```

The npm package is a distribution layer for the existing Go binaries. It must
not replace the Go CLI implementation. The package should work across supported
client platforms and should include the Linux daemon binaries needed by
`remork setup` and `remork daemon install`.

## Package Name and Versioning

- npm package name: `remork`
- npm install path: `npm install -g remork`
- installed command: `remork`
- GitHub tags use npm-friendly semver with a leading `v`, for example
  `v0.1.1-beta.4`.
- npm versions omit the leading `v`, for example `0.1.1-beta.4`.
- `remork version` should also omit the leading `v`, for example
  `remork 0.1.1-beta.4`.

## Package Layout

The npm package should contain all currently supported client binaries and
Linux daemon binaries:

```text
npm/remork/
├── package.json
├── README.md
├── bin/
│   └── remork.js
└── vendor/
    ├── remork-darwin-arm64
    ├── remork-darwin-amd64
    ├── remork-windows-arm64.exe
    ├── remork-windows-amd64.exe
    ├── remorkd-linux-arm64
    └── remorkd-linux-amd64
```

`package.json` should expose only the Node wrapper:

```json
{
  "name": "remork",
  "version": "0.1.1-beta.4",
  "bin": {
    "remork": "bin/remork.js"
  }
}
```

Use `package.json.files` as a whitelist so only the wrapper, vendor binaries,
README, license, and package metadata are published.

## Runtime Wrapper

`bin/remork.js` is a thin Node wrapper. It should:

1. Select the native client binary from `process.platform` and `process.arch`.
2. Forward user arguments unchanged.
3. Preserve stdin, stdout, stderr, signals, and exit code behavior.
4. Set `REMORK_DAEMON_VENDOR_DIR` to the package `vendor/` directory before
   spawning the native Go binary.
5. Print a clear error if the current client platform is unsupported or the
   expected binary is missing.

The wrapper should not implement Remork behavior. The Go binary remains the
single source of CLI behavior.

## Daemon Binary Resolution

When `remork setup` or `remork daemon install` needs a server daemon binary,
the Go CLI should resolve it in this order:

1. Use explicit `--local-bin` when provided.
2. If `REMORK_DAEMON_VENDOR_DIR` is set, look for the target platform daemon:
   - `remorkd-linux-arm64`
   - `remorkd-linux-amd64`
3. If the target platform is unknown in an interactive setup or daemon TUI
   context, ask the user to choose between `linux-arm64` and `linux-amd64`.
4. If the detected platform is unsupported, interactive flows should explain
   that the npm package only includes `linux-arm64` and `linux-amd64`; the user
   may manually choose a compatible platform. Non-interactive flows should fail
   with a clear message:

   ```text
   could not select remorkd platform; pass --platform linux-arm64 or linux-amd64
   ```

5. If the vendor directory is absent or does not contain the required daemon
   binary, fall back to the existing local cache, `dist/`, and GitHub Release
   resolution logic.

This keeps npm-specific paths out of the Go CLI. The Go CLI only depends on the
generic `REMORK_DAEMON_VENDOR_DIR` contract, which other distribution methods
can also reuse later.

## Build and Release Flow

The release flow should have two explicit build steps:

```bash
scripts/build-release.sh v0.1.1-beta.4
scripts/build-npm-package.sh v0.1.1-beta.4
```

`scripts/build-release.sh` continues to build GitHub Release binaries and
checksums.

`scripts/build-npm-package.sh` should:

1. Convert tag `v0.1.1-beta.4` to npm version `0.1.1-beta.4`.
2. Ensure `dist/` contains the required client and daemon binaries.
3. Create or refresh `npm/remork/`.
4. Copy client and daemon binaries into `npm/remork/vendor/`.
5. Copy the Node wrapper into `npm/remork/bin/remork.js`.
6. Generate `npm/remork/package.json`.
7. Generate a concise npm README.
8. Run `npm pack --dry-run` from `npm/remork`.

Initial npm publishing should be manual:

```bash
npm login
npm publish npm/remork
```

After one or two successful manual beta releases, npm publishing can be moved
into GitHub Actions for tag builds.

## Testing

Go tests should cover daemon binary resolution:

- `--local-bin` overrides all other sources.
- `REMORK_DAEMON_VENDOR_DIR` is preferred when it contains the requested daemon
  binary.
- Missing vendor binaries fall back to existing resolution.
- Non-interactive unknown platform returns a clear error.
- Interactive setup can ask the user to choose `linux-arm64` or `linux-amd64`
  when automatic platform selection is not available.

Node wrapper tests should cover:

- Platform mapping for macOS arm64, macOS amd64, Windows arm64, and Windows
  amd64.
- Argument forwarding.
- stdin/stdout/stderr passthrough.
- exit code passthrough.
- `REMORK_DAEMON_VENDOR_DIR` injection.
- clear errors for unsupported or missing client binaries.

Package smoke tests should cover:

```bash
npm pack --dry-run
npm install -g ./remork-*.tgz
remork version
remork setup --help
remork daemon install --help
```

Run these locally on macOS for the first implementation. CI can validate package
contents and wrapper mapping. Windows runtime smoke can be added once a Windows
runner is wired into release validation.

## Package Size

The first npm package should use a single package containing all supported
client and daemon binaries. Current binaries are small enough for this beta
distribution model, and the approach avoids install-time GitHub downloads.

If package size becomes a practical issue later, split into optional
per-platform packages. Do not introduce that complexity in the first npm
release.

## Documentation

The top-level README should add npm as the primary install path:

```bash
npm install -g remork
remork setup
```

The existing curl and PowerShell install instructions can remain as advanced or
manual fallback options.

The npm package README should be shorter than the repository README and focus
on:

- install
- setup
- daily commands
- supported platforms
- troubleshooting unsupported platforms

## Success Criteria

- `npm install -g remork` installs a working `remork` command.
- `remork version` prints a version without a leading `v`.
- `remork setup` can install a Linux daemon using the daemon binaries packaged
  in npm.
- Package install does not require downloading binaries from GitHub.
- `npm pack --dry-run` shows only intended package files.
- Existing GitHub Release binary workflow keeps working.
