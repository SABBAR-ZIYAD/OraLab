#!/usr/bin/env bash

set -euo pipefail

repository="SABBAR-ZIYAD/OraLab"
requested_os=""
requested_version=""

usage() {
  cat <<'EOF'
Usage: bash ./install.sh [--os linux|darwin|windows] [--version VERSION]

Without --os, OraLab shows an arrow-key operating-system menu.
The selected operating system must match the computer running the installer.
EOF
}

while (($# > 0)); do
  case "$1" in
    --os)
      if (($# < 2)); then
        echo "install.sh: --os requires a value" >&2
        exit 2
      fi
      requested_os="$2"
      shift 2
      ;;
    --version)
      if (($# < 2)); then
        echo "install.sh: --version requires a value" >&2
        exit 2
      fi
      requested_version="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "install.sh: unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

detect_os() {
  case "$(uname -s)" in
    Linux*) printf '%s' "linux" ;;
    Darwin*) printf '%s' "darwin" ;;
    MINGW*|MSYS*|CYGWIN*) printf '%s' "windows" ;;
    *) printf '%s' "unknown" ;;
  esac
}

select_os() {
  local detected="$1"
  local choices=("Windows (Intel/AMD 64-bit)" "Linux (Intel/AMD 64-bit)" "macOS (Intel 64-bit)")
  local values=("windows" "linux" "darwin")
  local selected=0
  local key=""
  local suffix=""

  case "$detected" in
    linux) selected=1 ;;
    darwin) selected=2 ;;
  esac

  echo "OraLab installation wizard"
  echo
  echo "Choose your operating system with Up/Down, then press Enter:"

  draw_menu() {
    local index
    for index in "${!choices[@]}"; do
      suffix=""
      if [[ "${values[$index]}" == "$detected" ]]; then
        suffix=" (detected)"
      fi
      if ((index == selected)); then
        printf '  > %s%s\033[K\n' "${choices[$index]}" "$suffix"
      else
        printf '    %s%s\033[K\n' "${choices[$index]}" "$suffix"
      fi
    done
  }

  draw_menu
  while true; do
    IFS= read -rsn1 key || true
    if [[ "$key" == $'\033' ]]; then
      local remainder=""
      IFS= read -rsn2 remainder || true
      key+="$remainder"
    fi
    case "$key" in
      "") break ;;
      $'\033[A') selected=$(((selected - 1 + ${#choices[@]}) % ${#choices[@]})) ;;
      $'\033[B') selected=$(((selected + 1) % ${#choices[@]})) ;;
      *) continue ;;
    esac
    printf '\033[%dA' "${#choices[@]}"
    draw_menu
  done
  SELECTED_OS="${values[$selected]}"
}

detected_os="$(detect_os)"
if [[ -n "$requested_os" ]]; then
  case "$requested_os" in
    windows|linux|darwin) selected_os="$requested_os" ;;
    *)
      echo "install.sh: --os must be windows, linux, or darwin" >&2
      exit 2
      ;;
  esac
else
  if [[ ! -t 0 ]]; then
    echo "install.sh: interactive input is unavailable; pass --os linux or --os darwin" >&2
    exit 2
  fi
  SELECTED_OS=""
  select_os "$detected_os"
  selected_os="$SELECTED_OS"
fi

if [[ "$selected_os" != "$detected_os" ]]; then
  echo "install.sh: selected $selected_os, but this shell is running on $detected_os" >&2
  echo "Run the installer on the computer where OraLab will be used and select that operating system." >&2
  exit 1
fi

if [[ "$selected_os" == "windows" ]]; then
  if ! command -v powershell.exe >/dev/null 2>&1; then
    echo "install.sh: PowerShell is required to install OraLab on Windows" >&2
    exit 1
  fi
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  if command -v cygpath >/dev/null 2>&1; then
    powershell_script="$(cygpath -w "$script_dir/install.ps1")"
  else
    powershell_script="$script_dir/install.ps1"
  fi
  arguments=(-NoLogo -NoProfile -ExecutionPolicy Bypass -File "$powershell_script" -OS windows)
  if [[ -n "$requested_version" ]]; then
    arguments+=(-Version "$requested_version")
  fi
  exec powershell.exe "${arguments[@]}"
fi

detected_architecture="$(uname -m)"
if [[ "$selected_os" == "darwin" ]] && command -v sysctl >/dev/null 2>&1 && \
  [[ "$(sysctl -n hw.optional.arm64 2>/dev/null || true)" == "1" ]]; then
  detected_architecture="arm64"
fi
case "$detected_architecture" in
  x86_64|amd64) ;;
  *)
    echo "install.sh: OraLab currently supports only Intel/AMD 64-bit computers." >&2
    echo "Detected architecture: $detected_architecture" >&2
    exit 1
    ;;
esac

for command_name in curl tar; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "install.sh: required command not found: $command_name" >&2
    exit 1
  fi
done

if [[ -n "$requested_version" ]]; then
  tag="v${requested_version#v}"
else
  release_json="$(curl -fsSL \
    -H 'Accept: application/vnd.github+json' \
    -H 'User-Agent: OraLab-Installer' \
    "https://api.github.com/repos/$repository/releases/latest")"
  tag="$(printf '%s\n' "$release_json" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
  if [[ -z "$tag" ]]; then
    echo "install.sh: could not determine the latest OraLab release" >&2
    exit 1
  fi
fi

version="${tag#v}"
asset="oralab_${version}_${selected_os}_amd64.tar.gz"
release_base="https://github.com/$repository/releases/download/$tag"
temporary_dir="$(mktemp -d)"
trap 'rm -rf -- "$temporary_dir"' EXIT

echo
echo "Downloading OraLab $version for $selected_os..."
curl -fL --progress-bar -o "$temporary_dir/$asset" "$release_base/$asset"
curl -fsSL -o "$temporary_dir/checksums.txt" "$release_base/checksums.txt"

expected_checksum="$(awk -v name="$asset" '$2 == name || $2 == "*" name { print $1; exit }' "$temporary_dir/checksums.txt")"
if [[ -z "$expected_checksum" ]]; then
  echo "install.sh: $asset is missing from checksums.txt" >&2
  exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  actual_checksum="$(sha256sum "$temporary_dir/$asset" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  actual_checksum="$(shasum -a 256 "$temporary_dir/$asset" | awk '{print $1}')"
else
  echo "install.sh: sha256sum or shasum is required to verify the download" >&2
  exit 1
fi
if [[ "$actual_checksum" != "$expected_checksum" ]]; then
  echo "install.sh: checksum verification failed for $asset" >&2
  exit 1
fi
echo "[OK] Download verified"

tar -xzf "$temporary_dir/$asset" -C "$temporary_dir"
if [[ ! -f "$temporary_dir/oralab" ]]; then
  echo "install.sh: the release archive does not contain oralab" >&2
  exit 1
fi

install_dir="$HOME/.local/bin"
mkdir -p "$install_dir"
install -m 0755 "$temporary_dir/oralab" "$install_dir/oralab"

path_line='export PATH="$HOME/.local/bin:$PATH"'
case "${SHELL:-}" in
  */zsh) profile="$HOME/.zshrc" ;;
  */bash) profile="$HOME/.bashrc" ;;
  *) profile="$HOME/.profile" ;;
esac
if [[ ":$PATH:" != *":$install_dir:"* ]]; then
  if [[ ! -f "$profile" ]] || ! grep -Fqx "$path_line" "$profile"; then
    printf '\n%s\n' "$path_line" >> "$profile"
  fi
  export PATH="$install_dir:$PATH"
  path_message=" Restart your shell to load the updated PATH from $profile."
else
  path_message=""
fi

echo "[OK] Installed $install_dir/oralab"
"$install_dir/oralab" version
echo
echo "OraLab is installed.$path_message"
echo "Next: create your lab directory and follow the README Quick start."

