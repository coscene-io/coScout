#!/usr/bin/env bash
# Copyright 2025 coScene
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.


set -Eeuo pipefail

# ANSI color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m' # No Color

echo_error() {
    echo -e "${RED}$*${NC}" >&2
}

echo_info() {
    echo -e "${GREEN}$*${NC}"
}

# Error handling function
error_handler() {
    local line_no=$1
    local error_msg=$2

    echo_error "⚠️ Error on line $line_no:"
    echo_error "⚠️ Error: $error_msg"

    exit 1
}

# Cleanup function
cleanup() {
    if [ ! -d "$TEMP_DIR" ]; then
        return
    fi
    echo "Cleaning up temp directory $TEMP_DIR"
    rm -rf "$TEMP_DIR"
}

# Set up traps
trap 'error_handler ${LINENO} "$BASH_COMMAND" "$?"' ERR
trap cleanup EXIT SIGINT SIGTERM

# check temp dir
TEMP_DIR=$(mktemp -d)
if [ ! -e "$TEMP_DIR" ]; then
  echo_error "Failed to create temp directory"
  exit 1
fi

# set os version
OS=$(uname -s)
case "$OS" in
Linux)
  OS="linux"
  ;;
*)
  echo_error "Unsupported OS: $OS. Only Linux is supported." >&2
  exit 1
  ;;
esac

# Set download ARCH based on system architecture
ARCH=$(uname -m)
case "$ARCH" in
x86_64)
  ARCH="amd64"
  ;;
arm64 | aarch64)
  ARCH="arm64"
  ;;
armv7l)
  ARCH="arm"
  ;;
*)
  echo_error "Unsupported architecture: $ARCH. Only x86_64, arm64, arm are supported." >&2
  exit 1
  ;;
esac

# Check if tar installed
if ! command -v tar &>/dev/null; then
  echo_error "tar is required but not installed. Please install it using: 'sudo apt-get install -y tar'" >&2
  exit 1
fi

# user input value
USE_LOCAL=""
BETA=0
DISABLE_SERVICE=0
MASTER_ADDR=""
PORT=22525
FILE_PREFIX=""
SLAVE_IP=""
SLAVE_ID=""

ARTIFACT_BASE_URL=https://download.coscene.cn
LATEST_COS_URL="${ARTIFACT_BASE_URL}/coscout/v2/latest/$OS-$ARCH.gz"
BETA_COS_URL="${ARTIFACT_BASE_URL}/coscout/v2/beta/$OS-$ARCH.gz"
LATEST_COS_INFO_URL="${ARTIFACT_BASE_URL}/coscout/v2/latest/$OS-$ARCH.json"
BETA_COS_INFO_URL="${ARTIFACT_BASE_URL}/coscout/v2/beta/$OS-$ARCH.json"

DEFAULT_INFO_URL="$LATEST_COS_INFO_URL"
DEFAULT_BINARY_URL="$LATEST_COS_URL"
# set binary_url based on beta flag
if [[ $BETA -eq 1 ]]; then
  DEFAULT_BINARY_URL="$BETA_COS_URL"
  DEFAULT_INFO_URL="$BETA_COS_INFO_URL"
fi

help() {
  cat <<EOF
usage: $0 [OPTIONS]

Install coScout as a slave node for master-slave architecture.

    --help                  Show this message
    --master_addr           Master node address (required, format: ip:port)
    --port                  Slave listening port (default: 22525)
    --file_prefix           File folder prefix for uploaded files (optional, e.g., 'device1')
    --ip                    IP address of this slave node (required)
    --slave_id              Slave ID (optional, auto-generated if not provided)
    --beta                  Use beta version for cos
    --use_local             Use local binary file zip path e.g. /xx/path/cos_binaries.tar.gz
    --disable_service       Disable systemd service installation
    --version               Show the version of the cos
EOF
}

error_exit() {
  echo_error "ERROR: $1" >&2
  exit 1
}

download_file() {
  local dest=$1
  local url=$2

  curl -SLo "$dest" "$url" || error_exit "Failed to download $url"
}

# Check if systemd is available
check_systemd() {
  if [[ "$(ps --no-headers -o comm 1 2>/dev/null)" == "systemd" ]]; then
    return 0
  else
    echo_error "This script requires systemd. For upstart systems, please use install-initd.sh instead."
    return 1
  fi
}

# get user input
while test $# -gt 0; do
  case $1 in
  --help)
    help
    exit 0
    ;;
  --master_addr=*)
    MASTER_ADDR="${1#*=}"
    shift # past argument=value
    ;;
  --port=*)
    PORT="${1#*=}"
    shift # past argument=value
    ;;
  --file_prefix=*)
    FILE_PREFIX="${1#*=}"
    shift # past argument=value
    ;;
  --ip=*)
    SLAVE_IP="${1#*=}"
    shift # past argument=value
    ;;
  --slave_id=*)
    SLAVE_ID="${1#*=}"
    shift # past argument=value
    ;;
  --beta)
    BETA=1
    shift # past argument
    ;;
  --use_local=*)
    USE_LOCAL="${1#*=}"
    shift # past argument=value
    ;;
  --disable_service)
    DISABLE_SERVICE=1
    shift # past argument
    ;;
  --version)
    VERSION_FILE="$(getent passwd "${USER:-$(whoami)}" | cut -d: -f6)/.local/state/cos/version.yaml"
    echo "read version from ${VERSION_FILE}"
    if [ -f "$VERSION_FILE" ]; then
      cat "$VERSION_FILE"
    else
      echo "no version file was found."
    fi
    exit 0
    ;;
  *)
    echo_error "unknown option: $1"
    help
    exit 1
    ;;
  esac
done

CUR_USER=${USER:-$(whoami)}
if [ -z "$CUR_USER" ]; then
  echo_error "can not get current user"
  exit 1
fi

echo "Current user: $CUR_USER"
CUR_USER_HOME=$(getent passwd "$CUR_USER" | cut -d: -f6)
if [ -z "$CUR_USER_HOME" ]; then
  echo_error "Cannot get home directory for user $CUR_USER"
  exit 1
fi
echo "User home directory: $CUR_USER_HOME"

# get user input
echo ""
if [[ -z "$MASTER_ADDR" ]]; then
  read -r -p "please input master_addr (format: ip:port): " MASTER_ADDR
fi
if [[ -z "$SLAVE_IP" ]]; then
  read -r -p "please input slave IP address: " SLAVE_IP
fi

echo "master_addr:   ${MASTER_ADDR}"
echo "port:          ${PORT}"
echo "file_prefix:   ${FILE_PREFIX}"
echo "ip:            ${SLAVE_IP}"
echo "slave_id:      ${SLAVE_ID}"

# Validate required parameters
if [[ -z "$MASTER_ADDR" ]]; then
  echo_error "ERROR: --master_addr must be provided. Exiting." >&2
  exit 1
fi

if [[ -z "$SLAVE_IP" ]]; then
  echo_error "ERROR: --ip must be provided. Exiting." >&2
  exit 1
fi

# check local file path
# Check if user specified local binary file
if [[ -n $USE_LOCAL ]]; then
  # Check if the file exists
  if [[ ! -f $USE_LOCAL ]]; then
    echo_error "ERROR: Specified file does not exist: $USE_LOCAL" >&2
    exit 1
  fi

  # Check if it is a tar.gz file
  if [[ ${USE_LOCAL: -7} != ".tar.gz" ]]; then
    echo_error "ERROR: The file specified is not a tar.gz archive. Exiting."
    exit 1
  fi

  # Extract files
  echo "Extracting $USE_LOCAL..."
  mkdir -p "$TEMP_DIR/cos_binaries"
  tar -xzf "$USE_LOCAL" -C "$TEMP_DIR/cos_binaries" || error_exit "Failed to extract $USE_LOCAL"
fi

echo ""
echo "Start install cos slave..."

# region config
COS_SHELL_BASE="$CUR_USER_HOME/.local"

# make some directories
COS_CONFIG_DIR="$CUR_USER_HOME/.config/cos"
COS_STATE_DIR="$CUR_USER_HOME/.local/state/cos"
COS_LOG_DIR="$CUR_USER_HOME/.local/state/cos/logs"
sudo -u "$CUR_USER" mkdir -p "$COS_CONFIG_DIR" "$COS_STATE_DIR" "$COS_SHELL_BASE/bin" "$COS_LOG_DIR"

# check old cos binary
if [ -e "$COS_SHELL_BASE/bin/cos" ]; then
  echo "Previously installed version:"
  "$COS_SHELL_BASE/bin/cos" --version || true
fi

# Check if user specified local binary file
if [[ -n $USE_LOCAL ]]; then
  TMP_FILE="$TEMP_DIR/cos_binaries/cos/$ARCH/$OS-$ARCH.gz"
  JSON_FILE="$TEMP_DIR/cos_binaries/cos/$ARCH/$OS-$ARCH.json"
  if [[ ! -f $TMP_FILE || ! -f $JSON_FILE ]]; then
    echo "ERROR: Failed to find cos binary or JSON file. Exiting."
    exit 1
  fi
  mv "$TEMP_DIR/cos_binaries/version.yaml" "$COS_STATE_DIR/version.yaml" 2>/dev/null || true
else
  mkdir -p "$TEMP_DIR/cos_binaries/cos"
  TMP_FILE="$TEMP_DIR/cos_binaries/cos/$OS-$ARCH.gz"
  JSON_FILE="$TEMP_DIR/cos_binaries/cos/$OS-$ARCH.json"
  download_file "$TMP_FILE" "$DEFAULT_BINARY_URL"
  download_file "$JSON_FILE" "$DEFAULT_INFO_URL"
fi

# Read SHA256 and Version from JSON file
REMOTE_SHA256=$(grep -o '"Sha256": [^"]*"[^"]*"' "$JSON_FILE" | sed 's/.*"Sha256": "\([^"]*\)".*/\1/')
VERSION=$(grep -o '"Version": [^"]*"[^"]*"' "$JSON_FILE" | sed 's/.*"Version": "\([^"]*\)".*/\1/')

if [[ -z $REMOTE_SHA256 || -z $VERSION ]]; then
  echo_error "Error: Failed to extract SHA256 or Version from JSON file. Exiting."
  exit 1
fi

# Function to decompress .gz file
decompress_gz() {
    if command -v gzip &> /dev/null; then
        gzip -cd "$1" > "$2"
    elif command -v gunzip &> /dev/null; then
        gunzip -c "$1" > "$2"
    else
        echo_error "Error: Neither gzip nor gunzip is available. Cannot decompress file."
        return 1
    fi
}

# Decompress and install
echo "Installing new cos version $VERSION:"
if decompress_gz "$TMP_FILE" "$TEMP_DIR/cos_binaries/cos/cos"; then
    # Verify SHA256
    LOCAL_SHA256=$(sha256sum "$TEMP_DIR/cos_binaries/cos/cos" | awk '{print $1}' | xxd -r -p | base64)
    if [[ "$REMOTE_SHA256" != "$LOCAL_SHA256" ]]; then
      echo_error "Error: SHA256 mismatch. Exiting."
      exit 1
    else
      echo "SHA256 verified. Proceeding with version $VERSION."
    fi

    mv -f "$TEMP_DIR/cos_binaries/cos/cos" "$COS_SHELL_BASE/bin/cos"
    sudo chmod +x "$COS_SHELL_BASE/bin/cos"
    echo "Installed cos version:"
    "$COS_SHELL_BASE/bin/cos" --version || true
else
    echo_error "Failed to decompress cos binary. Exiting."
    exit 1
fi

# Build command arguments
SLAVE_CMD_ARGS="slave --master-addr=${MASTER_ADDR} --port=${PORT} --ip=${SLAVE_IP}"
if [[ -n "$FILE_PREFIX" ]]; then
  SLAVE_CMD_ARGS="${SLAVE_CMD_ARGS} --file-prefix=\"${FILE_PREFIX}\""
fi
if [[ -n "$SLAVE_ID" ]]; then
  SLAVE_CMD_ARGS="${SLAVE_CMD_ARGS} --slave-id=${SLAVE_ID}"
fi

# check disable systemd, default will install cos.service
if [[ $DISABLE_SERVICE -eq 0 ]]; then
  if check_systemd; then
    echo "Installing cos slave systemd service..."

    # Create system-level systemd service file
    echo "Creating cos-slave.service systemd file..."

    sudo tee /etc/systemd/system/cos-slave.service >/dev/null <<EOL
[Unit]
Description=coScout Slave: Data Collector Slave Node by coScene
Documentation=https://github.com/coscene-io/coScout
StartLimitBurst=10
StartLimitIntervalSec=86400

[Service]
Type=simple
User=$CUR_USER
Group=$CUR_USER
WorkingDirectory=$CUR_USER_HOME/.local/state/cos
CPUQuota=10%
ExecStart=$COS_SHELL_BASE/bin/cos ${SLAVE_CMD_ARGS} --log-dir=${COS_LOG_DIR}
SyslogIdentifier=cos-slave
RestartSec=60
Restart=always

[Install]
WantedBy=multi-user.target
EOL
    echo "Created cos-slave.service systemd file: /etc/systemd/system/cos-slave.service"

    echo "Enabling linger for $CUR_USER..."
    sudo loginctl enable-linger "$CUR_USER"

    echo ""
    echo "Starting cos slave service for $CUR_USER..."

    echo "Reloading systemd daemon..."
    sudo systemctl daemon-reload

    echo "Checking if cos-slave service is running..."
    sudo systemctl is-active --quiet cos-slave && sudo systemctl stop cos-slave && sudo systemctl disable cos-slave

    echo "Enabling cos-slave service..."
    sudo systemctl enable cos-slave

    echo "Starting cos-slave service..."
    if sudo systemctl start cos-slave; then
      echo "Cos slave service started successfully."
    else
      echo_error "Cos slave service failed to start."

      echo_error "Checking service status for more details..."
      sudo systemctl status cos-slave || true
      echo_error "Checking journal logs for more details..."
      sudo journalctl -xe -u cos-slave --no-pager | tail -n 50
    fi

    echo ""
    echo_info "🎉 Installation completed successfully, you can use 'tail -f ${COS_LOG_DIR}/cos.log' to check the logs."
  else
    echo_error "This script requires systemd. For upstart systems, please use install-initd.sh instead."
    exit 1
  fi
else
  echo "Skipping systemd service installation, just install cos binary..."
  echo "You can manually start cos slave with the command: $COS_SHELL_BASE/bin/cos ${SLAVE_CMD_ARGS} --log-dir=${COS_LOG_DIR}"
fi

VERSION_FILE="$COS_STATE_DIR/version.yaml"
sudo tee "${VERSION_FILE}" > /dev/null << EOF
# coScene Edge Software Package Versions
# Generated on: $(date -u +"%Y-%m-%d %H:%M:%S UTC")

release_version: online
assemblies:
  cos_version: ${VERSION}
EOF

echo_info "Successfully installed cos slave."
exit 0

