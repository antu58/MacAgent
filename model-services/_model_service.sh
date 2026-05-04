#!/usr/bin/env bash
set -euo pipefail

MODEL_SERVICES_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MACAGENT_ROOT="$(cd "$MODEL_SERVICES_DIR/.." && pwd)"

print_model_service_usage() {
  local script_name="$1"
  local label="$2"
  local endpoint="$3"

  cat <<EOF
Usage:
  ./model-services/$script_name [action]

Model:
  $label

Endpoint:
  $endpoint

Actions:
  start       Stop any previous process, then launch the service in the background. Default.
  status      Show whether the service is running.
  stop        Stop the service.
  restart     Stop then start the service.
  setup       Install dependencies and download/reuse model weights.
  foreground  Run the server in the current shell.
  logs        List log files for this model service.
  tail        Follow the newest log file.
  help        Show this help.

Aliases:
  run, serve  Same as start.
  dev         Same as foreground.
  log         Same as logs.
EOF
}

service_script() {
  local service_dir="$1"
  local script_name="$2"
  printf '%s/scripts/%s\n' "$service_dir" "$script_name"
}

ensure_model_service_dir() {
  local service_dir="$1"
  if [[ ! -d "$service_dir" ]]; then
    echo "Model service directory not found: $service_dir" >&2
    exit 1
  fi
}

run_model_service_script() {
  local service_dir="$1"
  local script_name="$2"
  local script_path
  script_path="$(service_script "$service_dir" "$script_name")"
  if [[ ! -x "$script_path" ]]; then
    echo "Required service script is missing or not executable: $script_path" >&2
    exit 1
  fi

  (cd "$service_dir" && "./scripts/$script_name")
}

list_model_service_logs() {
  local service_dir="$1"
  local log_dir="$service_dir/run"
  if [[ ! -d "$log_dir" ]]; then
    echo "No log directory yet: $log_dir"
    return 0
  fi

  local found=0
  local log_file
  for log_file in "$log_dir"/*.log; do
    if [[ -f "$log_file" ]]; then
      found=1
      printf '%s\n' "$log_file"
    fi
  done

  if [[ "$found" == "0" ]]; then
    echo "No log files found in $log_dir"
  fi
}

tail_model_service_log() {
  local service_dir="$1"
  local log_dir="$service_dir/run"
  if [[ ! -d "$log_dir" ]]; then
    echo "No log directory yet: $log_dir" >&2
    exit 1
  fi

  local newest
  newest="$(find "$log_dir" -maxdepth 1 -type f -name '*.log' -print0 | xargs -0 ls -t 2>/dev/null | head -n 1 || true)"
  if [[ -z "${newest:-}" ]]; then
    echo "No log files found in $log_dir" >&2
    exit 1
  fi

  echo "Tailing: $newest"
  tail -n 80 -f "$newest"
}

model_service_main() {
  local script_name="$1"
  local label="$2"
  local service_rel="$3"
  local endpoint="$4"
  shift 4

  local action="${1:-start}"
  local service_dir="$MACAGENT_ROOT/$service_rel"
  ensure_model_service_dir "$service_dir"

  case "$action" in
    start|run|serve)
      echo "Starting $label"
      echo "Endpoint: $endpoint"
      run_model_service_script "$service_dir" "start_service.sh"
      ;;
    status)
      echo "$label"
      echo "Endpoint: $endpoint"
      run_model_service_script "$service_dir" "status_service.sh"
      ;;
    stop)
      echo "Stopping $label"
      run_model_service_script "$service_dir" "stop_service.sh"
      ;;
    restart)
      "$MODEL_SERVICES_DIR/$script_name" stop
      "$MODEL_SERVICES_DIR/$script_name" start
      ;;
    setup)
      echo "Setting up $label"
      run_model_service_script "$service_dir" "setup_mlx_service.sh"
      ;;
    foreground|dev)
      echo "Running $label in the current shell"
      echo "Endpoint: $endpoint"
      run_model_service_script "$service_dir" "start_mlx_service.sh"
      ;;
    logs|log)
      list_model_service_logs "$service_dir"
      ;;
    tail)
      tail_model_service_log "$service_dir"
      ;;
    help|-h|--help)
      print_model_service_usage "$script_name" "$label" "$endpoint"
      ;;
    *)
      echo "Unknown action: $action" >&2
      echo >&2
      print_model_service_usage "$script_name" "$label" "$endpoint" >&2
      exit 2
      ;;
  esac
}
