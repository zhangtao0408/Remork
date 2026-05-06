# Remork Setup-First TUI Design

## Status

Approved design baseline from the May 6, 2026 brainstorming session.

This spec defines a setup-first human TUI model for Remork while preserving the
existing scriptable command surface. It intentionally does not implement the
change.

## Goals

- Make `remork setup` the primary human entry point for first-use, server
  preparation, daemon updates, workspace connection, and repair.
- Keep daily commands direct and command-line friendly.
- Preserve `daemon install`, `daemon upgrade`, `host add`, and `init` as
  advanced primitives for scripts, agents, and troubleshooting.
- Prevent duplicated setup logic by routing product flows and advanced commands
  through shared operation specs and shared validation.
- Give human output a consistent productized terminal style, including one
  loading/progress vocabulary across setup, sync, pull, apply, run, watch, and
  shell.

## Non-Goals

- Do not delete existing commands.
- Do not turn every daily command into a form.
- Do not make default output bilingual.
- Do not implement full i18n in this pass.
- Do not rewrite core remote workspace semantics.

## Product Command Model

`remork setup` becomes the primary human-facing setup command.

Its top-level intents are:

- `Connect this project`
- `Only prepare a server`
- `Update an existing server`
- `Repair setup`

The existing lower-level commands remain available:

- `remork daemon install`
- `remork daemon upgrade`
- `remork host add`
- `remork init`

Those commands move out of the main discovery path. Root help and the root
interactive menu should promote `setup` and daily workflow commands first.
Daemon install/upgrade, host add, and init should be treated as advanced
primitives in help text, advanced docs, and troubleshooting flows.

## Setup Scope

When `remork setup` runs in an unbound directory, it must not assume that the
current directory should become a workspace.

It first asks what the user is setting up:

- `Connect this project`: prepare or choose a daemon, configure the local host,
  bind the current directory to a remote workspace, then offer first sync.
- `Only prepare a server`: install or update `remorkd`, configure the local
  host profile, verify status, then print the next `remork init` command.
- `Repair an existing setup`: run checks for host config, daemon reachability,
  auth, roots, and workspace binding, then offer targeted repair actions.

When `remork setup` runs in a bound directory, it should show the current
binding and prefer update, repair, and reconfigure flows.

## Shared Operation Core

`setup` must not duplicate daemon, host, init, or repair logic.

The CLI should extract typed operation specs that both product flows and
advanced commands can construct:

- `DaemonDeploySpec`
- `HostConfigSpec`
- `WorkspaceBindSpec`
- `DoctorRepairSpec` when repair actions are implemented

Each spec should pass through the same internal pipeline:

- defaults: fill derived values such as remote binary path, platform,
  configured host URL, token env, proxy mode, and existing host values.
- validate: enforce path rules, daemon URL rules, token/auth requirements,
  network bind safety, binary availability, and command-specific invariants.
- plan: generate the typed list of actions and risks that can be rendered,
  tested, and confirmed.
- execute: perform the planned SSH, SCP, config writes, daemon status checks,
  workspace binding, sync, or repair actions.

This pipeline is an implementation boundary, not a user-visible flow.

Tests should prove that setup paths and corresponding advanced commands produce
equivalent plans for the same inputs. Security validation must be impossible to
bypass by entering through `setup`.

## Interaction Model

Heavy interactivity is reserved for setup, configuration, dangerous operations,
and error recovery.

`remork setup` uses this flow:

1. Select scope.
2. Select intent.
3. Collect the minimum required fields for that intent.
4. Show a review plan with target, derived defaults, risks, and actions.
5. Confirm and execute.
6. Show progress, result, and next commands.

Long all-field forms should only appear behind an Advanced edit screen. The
default first screen should never be a raw flag list.

Daily commands remain direct:

- `pull <path>`
- `run command`
- `conflict PATH`
- `diff [path]`
- `status`
- `log`

When these commands lack required arguments, they should show friendly help and
examples instead of opening a TUI form.

Risky operations still prompt or require explicit confirmation:

- `apply`
- large-file pull or dirty overwrite
- setup and daemon deployment execution
- repair actions that mutate config or remote state

## Output Style

Default human output uses a productized clear style.

Rules:

- Default language is English.
- Commands, flags, paths, hosts, URLs, and machine terms stay in English.
- Do not mix English and Chinese on every output line.
- Future localization can add an explicit `--lang` flag or environment-driven
  language mode, but this pass keeps one English default.
- Use one stable primary color in the teal/green-blue family for product state,
  section titles, and the active step.
- Use semantic colors for success, warning, error, and command hints.
- Use bold only for section titles, important status labels, active actions,
  and risk headings.
- Keep JSON, quiet, non-interactive, dumb terminal, and no-color output stable,
  static, and script-friendly.

## Loading And Progress Vocabulary

All loading or running states use the same symbols.

```text
queued    ·
running   . o O ° O o .
done      ✓
failed    ×
skipped   -
```

The running frames are:

```text
[".", "o", "O", "°", "O", "o", "."]
```

Use this spinner only in rich TTY output. Dumb terminals, non-TTY output, JSON,
quiet mode, and non-interactive mode must not depend on animation. ASCII or
plain fallbacks can use `*` or static words such as `running`.

The implementation should test that every spinner frame has stable display
width in supported terminals, especially `°`.

## Command Coverage For Loading

Use one vocabulary, but choose the correct progress shape per command.

### Setup And Daemon Deploy

Use finite step tracks:

- detect or confirm platform
- prepare remote directories
- stop existing daemon when needed
- copy `remorkd`
- mark executable
- start daemon
- write or update host config
- verify daemon status
- optionally bind workspace
- optionally run initial sync

### Sync

Use a step track with counted progress where available:

- load local state
- scan local changes
- fetch remote manifest
- apply remote changes with operation count and progress bar
- save local state

### Pull

Use a step track with counted progress:

- load local state
- scan local changes
- fetch remote manifest
- confirm large-file or overwrite risk when needed
- download or write metadata
- save local state

### Apply

Use a finite step track:

- build plan
- confirm
- apply changes remotely
- refresh local state
- report conflicts or partial failure if needed

Do not invent per-file upload progress unless the apply pipeline exposes that
data.

### Run

Use short loading for preflight and automatic sync. Once the remote command is
running, stdout and stderr take priority. Spinners must not mix into command
stdout.

### Watch

Use connection state plus event logs. Do not keep a spinner alive for the entire
watch session. Show event lines, reconnect warnings, and short sync bursts.

### Shell

Show connection state before entering the shell. Once the interactive remote
shell starts, the CLI yields to the native PTY experience and stops rendering
TUI progress.

## Plan And Execution Rendering

Plan preview should show:

- title
- concise summary
- target values
- risks
- action list
- exact command hints when useful
- final confirmation prompt

Execution should show the same action list as a step track. Queued steps are
dim. The active step gets the only animated marker. Completed steps are
subdued. Failed steps are red and keep the rest of the track visible.

Failure output should include:

- the failed action
- reason
- next steps
- safe retry command where possible

## Help And Discovery

Root help should move from daemon-first setup language to setup-first language.

Suggested main groups:

- Setup: `setup`
- Daily: `sync`, `status`, `diff`, `apply`, `pull`, `run`, `shell`
- Observe: `log`, `watch`
- Diagnose: `doctor`
- Advanced: `daemon`, `host`, `workspace`, `debug`, `init`

The root interactive menu should follow the same grouping. When the current
directory is unbound, `setup` should be the first suggested command.

## Testing Strategy

Add tests for:

- `setup` intent selection and scope selection.
- each `setup` intent producing the expected typed specs.
- setup and advanced commands producing equivalent plans for equivalent input.
- daemon deployment safety validation through both entry points.
- root help and root menu promoting `setup`.
- advanced commands remaining available.
- no-color, `NO_COLOR`, `TERM=dumb`, JSON, quiet, non-interactive, and non-TTY
  output stability.
- action track symbols and spinner frames.
- sync, pull, apply, run, watch, and shell loading behavior following this
  spec.

## Rollout Notes

Implement in layers:

1. Extract shared operation specs and plan rendering without changing behavior.
2. Add shared action/progress rendering.
3. Add `remork setup` flows using the shared specs.
4. Move help and root menu to setup-first discovery.
5. Apply loading/progress style across sync, pull, apply, run, watch, and shell.
6. Update README, README_ZH, and the Remork skill docs.

This order keeps compatibility intact while reducing the risk of a second setup
implementation.
