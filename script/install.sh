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

## check root user
#if [[ "$EUID" -ne 0 ]]; then
#  echo "Please run as root user" >&2
#  exit 1
#fi

# ANSI color codes
RED='\033[0;31m'
NC='\033[0m' # No Color

echo_error() {
    echo -e "${RED}$*${NC}" >&2
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
    echo "Cleaning up temp directory $TEMP_DIR"
    [[ -n "$TEMP_DIR" && -d "$TEMP_DIR" ]] && rm -rf "$TEMP_DIR"
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
COLINK_ARCH=""
case "$ARCH" in
x86_64)
  ARCH="amd64"
  COLINK_ARCH="amd64"
  ;;
arm64 | aarch64)
  ARCH="arm64"
  COLINK_ARCH="aarch64"
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

# Check if nc installed
if ! command -v nc &>/dev/null; then
  echo_error "nc is required but not installed. Please install it using: 'sudo apt-get install -y netcat'" >&2
  exit 1
fi

# default value
DEFAULT_IMPORT_CONFIG=cos://organizations/current/configMaps/device.collector

# user input value
SERVER_URL=""
PROJECT_SLUG=""
ORG_SLUG=""
USE_LOCAL=""
BETA=0
DISABLE_SERVICE=0
REMOVE_CONFIG=0
MOD="default"
SN_FILE=""
SN_FIELD=""
COLINK_NETWORK=""
SERIAL_NUM=""
USE_32BIT=0
SKIP_VERIFY_CERT=0
COLINK_ENDPOINT=""
SSH_PORT=""
CONTROL_PORT=""

COLINK_VERSION=1.0.2
ARTIFACT_BASE_URL=https://coscene-artifacts-production.oss-cn-hangzhou.aliyuncs.com
COLINK_DOWNLOAD_URL=${ARTIFACT_BASE_URL}/colink/v${COLINK_VERSION}/colink-${COLINK_ARCH}
TRZSZ_DOWNLOAD_URL=${ARTIFACT_BASE_URL}/trzsz/v1.1.6/trzsz_1.1.6_linux_${COLINK_ARCH}.tar.gz

# cgroup path
GROUP_NAME="cos_cpu_limited"
CPU_PERCENT=15
CGROUP_PATH="/sys/fs/cgroup/cpu"

help() {
  cat <<EOF
usage: $0 [OPTIONS]

    --help                  Show this message
    --server_url            Api server url, e.g. https://openapi.coscene.cn
    --project_slug          The slug of the project to upload to
    --org_slug              The slug of the organization device belongs to, project_slug or org_slug should be provided
    --remove_config         Remove all config files, current device will be treated as a new device
    --beta                  Use beta version for cos
    --use_local             Use local binary file zip path e.g. /xx/path/xx.zip
    --disable_service       Disable systemd or upstart service installation
    --mod                   Select the mod to install - task, default or other custom mod (default is 'default')
    --sn_file               The file path of the serial number file, will skip if not provided
    --sn_field              The field name of the serial number, should be provided with sn_file, unique field to identify the device
    --serial_num            The serial number of the device, will skip sn_field and sn_file if provided
    --coLink_endpoint       coLink endpoint, e.g. https://api.mesh.staging.coscene.cn/mesh, will skip if not provided
    --coLink_network        coLink network id, e.g. organization id, will skip if not provided
    --use_32bit             Use 32-bit version for cos
    --skip_verify_cert      Skip verify certificate when download files
    --ssh_port              The port of the local ssh service
    --control_port          The port of the local control service
EOF
}

get_user_input() {
  local varname="$1"
  local prompt="$2"
  local inputValue="$3"

  while [[ -z ${inputValue} ]]; do
    read -r -p "${prompt}" inputValue
    if [[ -n ${inputValue} ]]; then
      eval "${varname}=\${inputValue}"
    fi
  done
}

error_exit() {
  echo_error "ERROR: $1" >&2
  exit 1
}

download_file() {
  local dest=$1
  local url=$2
  local skip_verify_cert=${3:-1} # Default to verifying the cert if not provided

  if [[ "$skip_verify_cert" -eq 1 ]]; then
    echo "Skip verify certificate when download file"
    curl -SLko "$dest" "$url" || error_exit "Failed to download $url without verifying the certificate"
  else
    curl -SLo "$dest" "$url" || error_exit "Failed to download $url"
  fi
}

check_cgroup_tools() {
  if ! command -v cgcreate &>/dev/null; then
    echo_error "Cannot install cgroup-tools automatically. Please install it manually 'apt-get install -y cgroup-tools'."
    return 1
  else
    echo "cgroup-tools is installed."
    return 0
  fi
}

# get user input
while test $# -gt 0; do
  case $1 in
  --help)
    help
    exit 0
    ;;
  --server_url=*)
    SERVER_URL="${1#*=}"
    shift # past argument=value
    ;;
  --project_slug=*)
    PROJECT_SLUG="${1#*=}"
    shift # past argument=value
    ;;
  --org_slug=*)
    ORG_SLUG="${1#*=}"
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
  --mod=*)
    mod_value="${1#*=}"
    shift
    # Check if the mod value is not empty
    if [[ -z $mod_value ]]; then
      echo_error "ERROR: --mod value cannot be empty. Exiting."
      exit 1
    else
      MOD="$mod_value"
    fi
    ;;
  --coLink_endpoint=*)
    COLINK_ENDPOINT="${1#*=}"
    shift
    ;;
  --sn_file=*)
    SN_FILE="${1#*=}"
    shift
    ;;
  --sn_field=*)
    SN_FIELD="${1#*=}"
    shift
    ;;
  --serial_num=*)
    SERIAL_NUM="${1#*=}"
    shift
    ;;
  --remove_config)
    REMOVE_CONFIG=1
    shift
    ;;
  --coLink_network=*)
    COLINK_NETWORK="${1#*=}"
    shift
    ;;
  --ssh_port=*)
    SSH_PORT="${1#*=}"
    shift
    ;;
  --control_port=*)
    CONTROL_PORT="${1#*=}"
    shift
    ;;
  --use_32bit)
    USE_32BIT=1
    shift # past argument
    ;;
  --skip_verify_cert)
    SKIP_VERIFY_CERT=1
    shift # past argument
    ;;
  *)
    echo_error "unknown option: $1"
    help
    exit 1
    ;;
  esac
done

if [[ $USE_32BIT -eq 1 ]]; then
  if [[ $ARCH != "arm64" ]] && [[ $ARCH != "arm" ]]; then
    echo_error "32-bit version is only supported on arm64 and arm architecture."
    exit 1
  fi
  ARCH="arm"
fi

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
get_user_input SERVER_URL "please input server_url: " "${SERVER_URL}"
echo "server_url is ${SERVER_URL}"
echo "org_slug is ${ORG_SLUG}"
echo "project_slug is ${PROJECT_SLUG}"
echo "coLink_endpoint is ${COLINK_ENDPOINT}"
echo "sn_file is ${SN_FILE}"
echo "sn_field is ${SN_FIELD}"
echo "serial_num is ${SERIAL_NUM}"

# check org_slug and project_slug
# Check if both ORG_SLUG and PROJECT_SLUG are empty
if [[ -z $ORG_SLUG && -z $PROJECT_SLUG ]]; then
  echo_error "ERROR: Both org_slug and project_slug cannot be empty. One of them must be specified. Exiting." >&2
  exit 1
fi

# Check if both ORG_SLUG and PROJECT_SLUG are not empty
if [[ -n $ORG_SLUG && -n $PROJECT_SLUG ]]; then
  echo_error "ERROR: Both org_slug and project_slug cannot be specified at the same time. Only one of them must be specified. Exiting."
  exit 1
fi

# check colink endpoint and network
if [[ -z "$COLINK_ENDPOINT" && -z "$COLINK_NETWORK" ]]; then
  echo "Both COLINK_ENDPOINT and COLINK_NETWORK are empty."
elif [[ -n "$COLINK_ENDPOINT" && -n "$COLINK_NETWORK" ]]; then
  echo "Both COLINK_ENDPOINT and COLINK_NETWORK are not empty."
else
  echo_error "ERROR: coLink_endpoint and coLink_network must either both be empty or both be not empty."
  exit 1
fi

# if mod is default, check
if [[ $MOD == "default" ]]; then

  # SN_FILE and SERIAL_NUM all empty, exit
  if [[ -z $SN_FILE && -z $SERIAL_NUM ]]; then
    echo_error "ERROR: Both sn_file and serial_num cannot be empty. One of them must be specified. Exiting."
    exit 1
  fi

  # check sn_file and sn_field
  # Check if SN_FILE is specified
  if [[ -n $SN_FILE ]]; then
    # Check if SN_FILE has valid extension
    valid_extensions=(.txt .json .yaml .yml)
    extension="${SN_FILE##*.}"
    if [[ ! " ${valid_extensions[*]} " =~ $extension ]]; then
      echo_error "ERROR: sn file has an invalid extension. Only .txt, .json, .yaml, .yml extensions are allowed. Exiting."
      exit 1
    fi

    # Check if SN_FILE exists
    if [[ ! -f $SN_FILE ]]; then
      echo_error "ERROR: sn file does not exist. Exiting."
      exit 1
    fi

    # Check if extension is not .txt and SN_FIELD is empty
    echo "extension is $extension"
    if [[ $extension != "txt" && -z $SN_FIELD ]]; then
      echo_error "ERROR: --sn_field is not specified when sn file exist. Exiting."
      exit 1
    fi
  fi
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
echo "Start install coLink..."
format() {
  local input=$1
  echo "${input//[\"|.]/}"
}

# check old colink binary
if [ -e /usr/local/bin/colink ]; then
  echo "Previously installed version:"
  /usr/local/bin/colink -V
fi

# check coLink endpoint or mesh arch
if [[ -z $COLINK_ENDPOINT ]] || [[ -z $COLINK_ARCH ]]; then
  echo "coLink endpoint and mesh arch are empty, skip coLink installation."
else
  if [[ -n $USE_LOCAL ]]; then
    echo "Moving new coLink binary..."
    mv -f "$TEMP_DIR/cos_binaries/colink/colink-${COLINK_ARCH}" "$TEMP_DIR"/colink
  else
    echo "Downloading new coLink binary..."
    download_file "$TEMP_DIR"/colink $COLINK_DOWNLOAD_URL $SKIP_VERIFY_CERT
  fi

  chmod +x "$TEMP_DIR"/colink
  echo "Installed new coLink version:"
  "$TEMP_DIR"/colink -V

  sudo mv -f "$TEMP_DIR"/colink /usr/local/bin/colink

  if [[ -n $USE_LOCAL ]]; then
    echo "Moving new trzsz binary..."
    cp "$TEMP_DIR/cos_binaries/trzsz_tar/trzsz_1.1.6_linux_${COLINK_ARCH}.tar.gz" "$TEMP_DIR"/trzsz.tar.gz
  else
    echo "Downloading new trzsz binary..."
    download_file "$TEMP_DIR"/trzsz.tar.gz $TRZSZ_DOWNLOAD_URL $SKIP_VERIFY_CERT
  fi

  echo "unzip trzsz..."
  mkdir -p "$TEMP_DIR"/trzsz
  tar -xzf "$TEMP_DIR"/trzsz.tar.gz -C "$TEMP_DIR"/trzsz --strip-components 1
  chmod -R +x "$TEMP_DIR"/trzsz
  sudo mv -f "$TEMP_DIR"/trzsz/* /usr/local/bin/
  rm -rf "$TEMP_DIR"/trzsz.tar.gz

  if [ -z "$SSH_PORT" ]; then
    # auto detect running ssh port
    if command -v netstat &> /dev/null; then
      SSH_PORT=$(sudo netstat -tlnp | grep sshd | head -1 | awk '{print $4}' | awk -F: '{print $2}')
    elif command -v ss &> /dev/null; then
      SSH_PORT=$(sudo ss -tlnp | grep sshd | head -1 | awk '{print $4}' | awk -F: '{print $2}')
    elif command -v lsof &> /dev/null; then
      SSH_PORT=$(sudo lsof -i -nP | grep sshd | grep LISTEN | grep IPv4 | head -1 | awk '{print $9}' | awk -F: '{print $2}')
    else
      echo_error "Unable to detect running ssh port, please specify --ssh_port manually."
      exit 1
    fi
    # check if SSH_PORT is a valid port number
    if ! [[ "$SSH_PORT" =~ ^[0-9]+$ ]] ; then
      echo_error "Invalid port number: $SSH_PORT"
      echo_error "Unable to detect running ssh port, please specify --ssh_port manually."
      exit 1
    fi
  fi

  if [ -z "$CONTROL_PORT" ]; then
    CONTROL_PORT=18880
    while [ $CONTROL_PORT -le 30000 ]; do
      if ! nc -z 127.0.0.1 $CONTROL_PORT; then
        break
      fi
      CONTROL_PORT=$((CONTROL_PORT + 1))
    done
    if [ $CONTROL_PORT -gt 30000 ]; then
      echo_error "Unable to find a free port for colink control api, please specify --control_port manually."
      exit 1
    fi
  fi


  # check systemd or upstart service
  if [[ $DISABLE_SERVICE -eq 0 ]]; then
    if [[ "$(ps --no-headers -o comm 1 2>&1)" == "systemd" ]] && command -v systemctl 2>&1; then
      echo "Installing systemd service..."
      sudo tee /etc/systemd/system/colink.service >/dev/null <<EOF

[Unit]
Description=coLink Client Daemon

[Service]
WorkingDirectory=/etc
ExecStart=/usr/local/bin/colink --endpoint ${COLINK_ENDPOINT} --network ${COLINK_NETWORK} --allow-ssh --ssh-port ${SSH_PORT} --control-port ${CONTROL_PORT}
Restart=always
RestartSec=30

[Install]
WantedBy=multi-user.target
EOF
      sudo systemctl daemon-reload

      echo "Starting coLink service..."
      sudo systemctl is-active --quiet coLink && sudo systemctl stop coLink && sudo systemctl disable coLink && sudo rm -f /etc/systemd/system/coLink.service
      sudo systemctl is-active --quiet colink && sudo systemctl stop colink && sudo systemctl disable colink && sudo rm -f /etc/systemd/system/colink.service
      sudo systemctl is-active --quiet virmesh && sudo systemctl stop virmesh && sudo systemctl disable virmesh && sudo rm -f /etc/systemd/system/virmesh.service
      sudo systemctl is-active --quiet colink && sudo systemctl stop colink
      sudo systemctl enable colink
      sudo systemctl start colink
      echo "Start coLink service done."
    elif /sbin/init --version 2>&1 | grep -q upstart; then
      echo "Installing upstart service..."
      sudo tee /etc/init/colink.conf >/dev/null <<EOF
description "coLink Client Daemon"

# Start the service when networking is up
start on started networking

# Stop the service when leaving runlevel 2, 3, 4, 5
stop on runlevel [!2345]

# Respawn the service if it crashes
respawn

# Limit respawn attempts to 4 within a 25 second period
respawn limit 4 30

# Consider exit code 0 as normal and not trigger a respawn
normal exit 0

env COLINK_ENDPOINT=$COLINK_ENDPOINT
env COLINK_NETWORK=$COLINK_NETWORK
script
    # Change to the appropriate working directory
    cd /etc
    # Start the daemon
    exec /usr/local/bin/colink --endpoint ${COLINK_ENDPOINT} --network ${COLINK_NETWORK} --allow-ssh
end script
EOF

      SERVICE_NAME="colink"
      STATUS_OUTPUT=$(sudo initctl status "$SERVICE_NAME")
      if echo "$STATUS_OUTPUT" | grep -q "start/running"; then
        echo "$SERVICE_NAME is running. Stopping it now..."
        sudo initctl stop "$SERVICE_NAME"
        echo "$SERVICE_NAME has been stopped."
      else
        echo "$SERVICE_NAME is not running."
      fi
      sudo initctl start $SERVICE_NAME
    fi
  else
    echo "Skipping systemd or upstart service installation, just install coLink binary..."
  fi
  echo "Successfully installed coLink."
fi

echo ""
echo "Start install cos..."

# remove old config before install
if [[ $REMOVE_CONFIG -eq 1 ]]; then
  echo "remove exists config file."
  rm -rf "$CUR_USER_HOME"/.local/state/cos
  rm -rf "$CUR_USER_HOME"/.config/cos
  rm -rf "$CUR_USER_HOME"/.cache/coscene
  rm -rf "$CUR_USER_HOME"/.cache/cos
fi

# set some variables
LATEST_COS_URL="https://download.coscene.cn/coscout/v2/latest/$OS-$ARCH.gz"
BETA_COS_URL="https://download.coscene.cn/coscout/v2/beta/$OS-$ARCH.gz"
LATEST_COS_INFO_URL="https://download.coscene.cn/coscout/v2/latest/$OS-$ARCH.json"
BETA_COS_INFO_URL="https://download.coscene.cn/coscout/v2/beta/$OS-$ARCH.json"

DEFAULT_INFO_URL="$LATEST_COS_INFO_URL"
DEFAULT_BINARY_URL="$LATEST_COS_URL"
# set binary_url based on beta flag
if [[ $BETA -eq 1 ]]; then
  DEFAULT_BINARY_URL="$BETA_COS_URL"
  DEFAULT_INFO_URL="$BETA_COS_INFO_URL"
fi

# region config
COS_SHELL_BASE="$CUR_USER_HOME/.local"

# make some directories
COS_CONFIG_DIR="$CUR_USER_HOME/.config/cos"
COS_STATE_DIR="$CUR_USER_HOME/.local/state/cos"
sudo -u "$CUR_USER" mkdir -p "$COS_CONFIG_DIR" "$COS_STATE_DIR" "$COS_SHELL_BASE/bin"

# check provide serial number
if [[ -n $SERIAL_NUM ]]; then
  echo "Provided serial number: $SERIAL_NUM"
  SN_FILE="$COS_CONFIG_DIR/cos_sn.yaml"
  SN_FIELD="serial_number"

  sudo -u "$CUR_USER" tee "${SN_FILE}" >/dev/null <<EOL
"$SN_FIELD": "$SERIAL_NUM"
EOL
fi

# create config file
echo "Creating config file..."
INSECURE=false
if [[ $SKIP_VERIFY_CERT -eq 1 ]]; then
  INSECURE=true
fi

# create config file ~/.config/cos/config.yaml
sudo -u "$CUR_USER" tee "${COS_CONFIG_DIR}/config.yaml" >/dev/null <<EOL
api:
  server_url: $SERVER_URL
  project_slug: $PROJECT_SLUG
  org_slug: $ORG_SLUG
  insecure: $INSECURE

register:
  type: file
  config:
    sn_file: $SN_FILE
    sn_field: $SN_FIELD

mod:
  name: $MOD
  conf:
    enabled: true

__import__:
  - $DEFAULT_IMPORT_CONFIG
  - ${COS_CONFIG_DIR}/local.yaml
EOL

# create local config file
LOCAL_CONFIG_FILE="${COS_CONFIG_DIR}/local.yaml"
if [[ ! -f "$LOCAL_CONFIG_FILE" ]]; then
  echo "{}" >"$LOCAL_CONFIG_FILE"
fi
echo "Created config file: ${COS_CONFIG_DIR}/config.yaml"
# endregion

check_binary() {
  cmd="$COS_SHELL_BASE/bin/${1}"
  if [[ ! -e "$cmd" ]]; then
    echo "$cmd not found, skip check."
    return 0
  fi

  echo -n "  - Checking ${1} executable ... "

  local output
  if ! output=$("$cmd" --version 2>&1); then
    echo_error "Error: $output"
  else
    echo "$output"
    return 0
  fi

  return 1
}

# check old cos binary
if [ -e "$COS_SHELL_BASE/bin/cos" ]; then
  echo "Previously installed version:"
  check_binary cos
fi

# Check if user specified local binary file
if [[ -n $USE_LOCAL ]]; then
  TMP_FILE="$TEMP_DIR/cos_binaries/cos/$OS-$ARCH.gz"
  JSON_FILE="$TEMP_DIR/cos_binaries/cos/$OS-$ARCH.json"
  if [[ ! -f $TMP_FILE || ! -f $JSON_FILE ]]; then
    echo "ERROR: Failed to find cos binary or JSON file. Exiting."
    exit 1
  fi
else
  mkdir -p "$TEMP_DIR/cos_binaries/cos"
  TMP_FILE="$TEMP_DIR/cos_binaries/cos/$OS-$ARCH.gz"
  JSON_FILE="$TEMP_DIR/cos_binaries/cos/$OS-$ARCH.json"
  download_file "$TMP_FILE" "$DEFAULT_BINARY_URL" $SKIP_VERIFY_CERT
  download_file "$JSON_FILE" "$DEFAULT_INFO_URL" $SKIP_VERIFY_CERT
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
    check_binary cos
else
    echo_error "Failed to decompress cos binary. Exiting."
    exit 1
fi

# check disable systemd, default will install cos.service
if [[ $DISABLE_SERVICE -eq 0 ]]; then
  if [[ "$(ps --no-headers -o comm 1 2>&1)" == "systemd" ]] && command -v systemctl 2>&1; then
    echo "Installing cos systemd service..."

    echo "Enabling linger for $CUR_USER..."
    sudo loginctl enable-linger "$CUR_USER"
    # create cos.service systemd file
    echo "Creating cos.service systemd file..."
    #  echo "Installing the systemd service requires root permissions."
    #  cat >/lib/systemd/system/cos.service <<EOL

    USER_SYSTEMD_DIR="$CUR_USER_HOME/.config/systemd/user"
    USER_DEFAULT_TARGET="$CUR_USER_HOME/.config/systemd/user/default.target.wants"
    sudo -u "$CUR_USER" mkdir -p "$USER_SYSTEMD_DIR" "$USER_DEFAULT_TARGET"
    sudo -u "$CUR_USER" tee "$USER_SYSTEMD_DIR"/cos.service >/dev/null <<EOL
[Unit]
Description=coScout: Data Collector by coScene
Documentation=https://github.com/coscene-io/sample-json-api-files
Wants=network-online.target
After=network.target network-online.target
StartLimitBurst=10
StartLimitIntervalSec=86400

[Service]
Type=simple
WorkingDirectory=$CUR_USER_HOME/.local/state/cos
StandardOutput=syslog
StandardError=syslog
CPUQuota=10%
ExecStart=$COS_SHELL_BASE/bin/cos daemon --config-path=${COS_CONFIG_DIR}/config.yaml
SyslogIdentifier=cos
RestartSec=60
Restart=always

[Install]
WantedBy=default.target
EOL
    echo "Created cos.service systemd file: $USER_SYSTEMD_DIR/cos.service"
    echo "Starting cos service for $CUR_USER..."

    XDG_RUNTIME_DIR="/run/user/$(id -u "${CUR_USER}")"
    sudo -u "$CUR_USER" XDG_RUNTIME_DIR="$XDG_RUNTIME_DIR" systemctl --user daemon-reload
    sudo -u "$CUR_USER" XDG_RUNTIME_DIR="$XDG_RUNTIME_DIR" systemctl --user is-active --quiet cos && sudo -u "$CUR_USER" XDG_RUNTIME_DIR="$XDG_RUNTIME_DIR" systemctl --user stop cos
    sudo -u "$CUR_USER" XDG_RUNTIME_DIR="$XDG_RUNTIME_DIR" systemctl --user enable cos
    sudo -u "$CUR_USER" XDG_RUNTIME_DIR="$XDG_RUNTIME_DIR" systemctl --user start cos
    echo "Start cos service done."

    echo "Installation completed successfully 🎉, you can use 'journalctl --user-unit=cos -f -n 50' to check the logs."
  elif /sbin/init --version 2>&1 | grep -q upstart; then
    echo "Installing cos upstart service..."

    if ! command -v cgcreate &>/dev/null; then
      if [[ -n $USE_LOCAL  ]] && [[ $ARCH == "arm" ]]; then
        echo "Installing cgroup-tools..."
        sudo dpkg -i "$TEMP_DIR/cos_binaries/cos/$ARCH/libcgroup1.deb"
        sudo dpkg -i "$TEMP_DIR/cos_binaries/cos/$ARCH/cgroup_lite.deb"
        sudo dpkg -i "$TEMP_DIR/cos_binaries/cos/$ARCH/cgroup_bin.deb"

        if ! command -v cgcreate &>/dev/null; then
          echo_error "Failed to install cgroup-tools."
          exit 1
        fi
      fi
    fi

    exec_command="exec $COS_SHELL_BASE/bin/cos daemon --config-path=${COS_CONFIG_DIR}/config.yaml"
    if check_cgroup_tools; then
      exec_command="exec cgexec -g cpu:$GROUP_NAME $COS_SHELL_BASE/bin/cos daemon --config-path=${COS_CONFIG_DIR}/config.yaml"
    fi

    sudo tee /etc/init/cos.conf >/dev/null <<EOF
description "coScout: Data Collector by coScene"
author "coScene"

start on started networking
stop on runlevel [!2345]

nice 19

# Limit the start attempts
respawn
respawn limit 10 86400

pre-start script
  rm -rf $CUR_USER_HOME/.cache/coscene/onefile_*

  if command -v cgcreate &>/dev/null; then
    if [ -d "$CGROUP_PATH/$GROUP_NAME" ]; then
      cgdelete cpu:$GROUP_NAME
    fi

    if ! cgcreate -g cpu:$GROUP_NAME; then
      echo "Failed to create cgroup"
      exit 1
    fi

    cgset -r cpu.cfs_period_us=100000 $GROUP_NAME
    cgset -r cpu.cfs_quota_us=$((CPU_PERCENT * 1000)) $GROUP_NAME
  fi
end script

script
    cd $CUR_USER_HOME/.local/state/cos
    $exec_command
end script

post-stop script
  # post-stop script
end script

# Logging settings
console log
EOF

    SERVICE_NAME="cos"
    STATUS_OUTPUT=$(sudo initctl status "$SERVICE_NAME")
    if echo "$STATUS_OUTPUT" | grep -q "start/running"; then
      echo "$SERVICE_NAME is running. Stopping it now..."
      sudo initctl stop "$SERVICE_NAME"
      echo "$SERVICE_NAME has been stopped."
    else
      echo "$SERVICE_NAME is not running."
    fi

    echo "reload upstart configuration..."
    sudo initctl reload-configuration
    sudo initctl start $SERVICE_NAME

    echo "Installation completed successfully 🎉, you can use 'tail -f /var/log/upstart/cos.log' to check the logs."
  fi
else
  echo "Skipping systemd service installation, just install cos binary..."
fi

echo "Successfully installed cos."
exit 0
