#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/remote-smoke.sh --host SSH_TARGET --root REMOTE_ALLOWED_ROOT --port PORT --binary dist/remorkd-linux-arm64 [options]

Options:
  --host SSH_TARGET       SSH target, for example user@my-server.
  --root REMOTE_ROOT      Remote allowed root to create and expose.
  --port PORT             Daemon port to listen on.
  --binary PATH           Local prebuilt remorkd binary to copy.
  --probe-host HOST       Hostname or IP used for local HTTP probes. Defaults to --host without user@.
  --remote-bin PATH       Remote daemon path. Defaults to .local/bin/remorkd-smoke under the remote home.
  --listen ADDR           Remote listen host. Defaults to 0.0.0.0.
  --keep                  Leave daemon and files in place; print cleanup command.

The remote host does not need Go, npm, apt, brew, or internet access.
EOF
}

host=""
root=""
port=""
binary=""
probe_host=""
remote_bin=".local/bin/remorkd-smoke"
listen_host="0.0.0.0"
keep="false"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --host) host="${2:-}"; shift 2 ;;
    --root) root="${2:-}"; shift 2 ;;
    --port) port="${2:-}"; shift 2 ;;
    --binary) binary="${2:-}"; shift 2 ;;
    --probe-host) probe_host="${2:-}"; shift 2 ;;
    --remote-bin) remote_bin="${2:-}"; shift 2 ;;
    --listen) listen_host="${2:-}"; shift 2 ;;
    --keep) keep="true"; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

if [ -z "$host" ] || [ -z "$root" ] || [ -z "$port" ] || [ -z "$binary" ]; then
  usage >&2
  exit 2
fi
if [ ! -x "$binary" ]; then
  echo "binary is not executable: $binary" >&2
  exit 2
fi
if [ -z "$probe_host" ]; then
  probe_host="${host#*@}"
fi

if [[ "$remote_bin" == /* ]]; then
  remote_exec_bin="$remote_bin"
  remote_scp_bin="$remote_bin"
  remote_bin_dir="$(dirname "$remote_bin")"
elif [[ "$remote_bin" == "~/"* ]]; then
  remote_exec_bin="\$HOME/${remote_bin#~/}"
  remote_scp_bin="$remote_bin"
  remote_bin_dir="\$HOME/$(dirname "${remote_bin#~/}")"
else
  remote_exec_bin="\$HOME/${remote_bin#./}"
  remote_scp_bin="${remote_bin#./}"
  remote_bin_dir="\$HOME/$(dirname "${remote_bin#./}")"
fi

pid_file="\$HOME/.remork/run/remorkd-smoke-${port}.pid"
log_file="\$HOME/.remork/log/remorkd-smoke-${port}.log"
url="http://${probe_host}:${port}"
status_file="$(mktemp)"

remote_cleanup="if [ -f \"$pid_file\" ]; then kill \"\$(cat \"$pid_file\")\" 2>/dev/null || true; rm -f \"$pid_file\"; fi; rm -f \"$remote_exec_bin\" \"$log_file\""
cleanup() {
  rm -f "$status_file"
  if [ "$keep" = "false" ]; then
    ssh "$host" "$remote_cleanup" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

echo "Preparing remote workspace $host:$root"
ssh "$host" "mkdir -p '$root' \"$remote_bin_dir\" \"\$HOME/.remork/run\" \"\$HOME/.remork/log\" && printf 'remork smoke\n' > '$root/remork-smoke.txt'"

echo "Copying $binary to $host:$remote_scp_bin"
scp "$binary" "$host:$remote_scp_bin" >/dev/null
ssh "$host" "chmod 0755 \"$remote_exec_bin\""

echo "Starting remorkd on $listen_host:$port"
ssh "$host" "$remote_cleanup; nohup \"$remote_exec_bin\" --root '$root' --addr '$listen_host:$port' </dev/null >\"$log_file\" 2>&1 & echo \$! > \"$pid_file\""

echo "Probing $url/status"
for _ in 1 2 3 4 5; do
  if curl --noproxy '*' -fsS "$url/status" >"$status_file"; then
    break
  fi
  sleep 1
done
curl --noproxy '*' -fsS "$url/status"
echo

echo "Probing $url/manifest"
curl --noproxy '*' -fsS --get "$url/manifest" --data-urlencode "root=$root" --data-urlencode "path=." --data-urlencode "recursive=true"
echo

cat <<EOF

Smoke passed.

Cleanup command:
  ssh $host "$remote_cleanup; rm -rf '$root/.remork'"
EOF

if [ "$keep" = "true" ]; then
  trap - EXIT
fi
