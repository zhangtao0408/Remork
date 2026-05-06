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
  --no-token              Start without a bearer token. Use only on trusted private networks.
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
use_token="true"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --host) host="${2:-}"; shift 2 ;;
    --root) root="${2:-}"; shift 2 ;;
    --port) port="${2:-}"; shift 2 ;;
    --binary) binary="${2:-}"; shift 2 ;;
    --probe-host) probe_host="${2:-}"; shift 2 ;;
    --remote-bin) remote_bin="${2:-}"; shift 2 ;;
    --listen) listen_host="${2:-}"; shift 2 ;;
    --no-token) use_token="false"; shift ;;
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
  resolved_probe_host="$(ssh -G "$host" 2>/dev/null | awk 'tolower($1) == "hostname" { print $2; exit }')"
  if [ -n "$resolved_probe_host" ]; then
    probe_host="$resolved_probe_host"
  else
    probe_host="${host#*@}"
  fi
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
token_file="\$HOME/.remork/run/remorkd-smoke-${port}.token"
url="http://${probe_host}:${port}"
status_file="$(mktemp)"
token=""
auth_args=()
remote_auth_args=""
daemon_auth_args=""
ssh_opts=(-o BatchMode=yes -o ConnectTimeout=10 -o ServerAliveInterval=5 -o ServerAliveCountMax=2)
scp_opts=(-o BatchMode=yes -o ConnectTimeout=10 -o ServerAliveInterval=5 -o ServerAliveCountMax=2)
if [ "$use_token" = "true" ]; then
  token="remork-smoke-${port}-$$-$(date +%s)"
  auth_args=(-H "Authorization: Bearer $token")
  remote_auth_args="-H 'Authorization: Bearer $token'"
  daemon_auth_args="--token-file \"$token_file\""
fi

remote_stop="if [ -f \"$pid_file\" ]; then kill \"\$(cat \"$pid_file\")\" 2>/dev/null || true; rm -f \"$pid_file\"; fi; rm -f \"$log_file\""
remote_cleanup="$remote_stop; rm -f \"$remote_exec_bin\" \"$token_file\" '$root/remork-smoke.txt'; rm -rf '$root/.remork'; rmdir '$root' 2>/dev/null || true"
cleanup() {
  rm -f "$status_file"
  if [ "$keep" = "false" ]; then
    ssh "${ssh_opts[@]}" "$host" "$remote_cleanup" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

echo "Preparing remote workspace $host:$root"
ssh "${ssh_opts[@]}" "$host" "mkdir -p '$root' \"$remote_bin_dir\" \"\$HOME/.remork/run\" \"\$HOME/.remork/log\" && printf 'remork smoke\n' > '$root/remork-smoke.txt'"

echo "Copying $binary to $host:$remote_scp_bin"
if ! scp "${scp_opts[@]}" "$binary" "$host:$remote_scp_bin" >/dev/null; then
  echo "Failed to copy daemon binary to $host:$remote_scp_bin." >&2
  echo "Check SSH BatchMode authentication, remote sftp/scp support, disk quota, and --remote-bin." >&2
  exit 1
fi
ssh "${ssh_opts[@]}" "$host" "chmod 0755 \"$remote_exec_bin\""

echo "Starting remorkd on $listen_host:$port"
if [ "$use_token" = "true" ]; then
  ssh "${ssh_opts[@]}" "$host" "printf '%s\n' '$token' > \"$token_file\" && chmod 0600 \"$token_file\""
fi
ssh "${ssh_opts[@]}" "$host" "$remote_stop; nohup \"$remote_exec_bin\" --root '$root' --addr '$listen_host:$port' $daemon_auth_args </dev/null >\"$log_file\" 2>&1 & echo \$! > \"$pid_file\""

echo "Probing $url/status"
status_ok="false"
for _ in 1 2 3 4 5; do
  if curl --noproxy '*' -fsS "${auth_args[@]}" "$url/status" >"$status_file"; then
    status_ok="true"
    break
  fi
  sleep 1
done
if [ "$status_ok" != "true" ]; then
  echo "Local probe failed for $url/status." >&2
  echo "Checking whether remorkd is reachable from the remote host itself..." >&2
  if ssh "${ssh_opts[@]}" "$host" "curl -fsS $remote_auth_args 'http://127.0.0.1:$port/status'" >&2; then
    echo >&2
    echo "remorkd is running on remote localhost, but this machine cannot reach $probe_host:$port." >&2
    echo "Check VPN routing, firewall rules, --probe-host, and --listen." >&2
  else
    echo "remorkd was not reachable from remote localhost. Remote log follows:" >&2
    ssh "${ssh_opts[@]}" "$host" "tail -n 80 \"$log_file\"" >&2 || true
  fi
  exit 1
fi
cat "$status_file"
echo

echo "Probing $url/manifest"
curl --noproxy '*' -fsS "${auth_args[@]}" --get "$url/manifest" --data-urlencode "root=$root" --data-urlencode "path=." --data-urlencode "recursive=true"
echo

cat <<EOF

Smoke passed.

Cleanup command:
EOF
printf '  %q' ssh "${ssh_opts[@]}" "$host" "$remote_cleanup"
echo

if [ "$keep" = "true" ]; then
  trap - EXIT
fi
