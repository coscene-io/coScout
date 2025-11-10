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
COLINK_ARCH=""
ROS_NODE_ARCH=""
case "$ARCH" in
x86_64)
  ARCH="amd64"
  COLINK_ARCH="amd64"
  ROS_NODE_ARCH="amd64"
  ;;
arm64 | aarch64)
  ARCH="arm64"
  COLINK_ARCH="aarch64"
  ROS_NODE_ARCH="arm64"
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
INSTALL_COLISTENER=0
INSTALL_COBRIDGE=0
ENABLE_MASTER=0
HTTP_PROXY=""
NO_PROXY="localhost,127.0.0.1,::1,.local,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16"

COLINK_VERSION=1.0.4
TRZSZ_VERSION=1.1.6
ARTIFACT_BASE_URL=https://download.coscene.cn
APT_BASE_URL=https://apt.coscene.cn
COLINK_DOWNLOAD_URL=${ARTIFACT_BASE_URL}/colink/v${COLINK_VERSION}/colink-${COLINK_ARCH}
TRZSZ_DOWNLOAD_URL=${ARTIFACT_BASE_URL}/trzsz/v${TRZSZ_VERSION}/trzsz_${TRZSZ_VERSION}_linux_${COLINK_ARCH}.tar.gz

help() {
  cat <<EOF
usage: $0 [OPTIONS]

    --help                  Show this message
    --server_url            Api server url
    --project_slug          The slug of the project to upload to
    --org_slug              The slug of the organization device belongs to, project_slug or org_slug should be provided
    --remove_config         Remove all config files, current device will be treated as a new device
    --beta                  Use beta version for cos
    --use_local             Use local binary file zip path e.g. /xx/path/cos_binaries.tar.gz
    --disable_service       Disable systemd service installation
    --mod                   Select the mod to install - task, default or other mod (default is 'default')
    --sn_file               The file path of the serial number file, will skip if not provided
    --sn_field              The field name of the serial number, should be provided with sn_file, unique field to identify the device
    --serial_num            The serial number of the device, will skip sn_field and sn_file if provided
    --coLink_endpoint       coLink endpoint, will skip if not provided
    --coLink_network        coLink network id, e.g. organization id, will skip if not provided
    --use_32bit             Use 32-bit version for cos
    --skip_verify_cert      Skip verify certificate when download files
    --install_colistener    Install colistener component (default: false)
    --install_cobridge      Install cobridge component (default: false)
    --enable_master         Enable master mode for master-slave architecture (default: false)
    --http_proxy            Set HTTP proxy for cos service (e.g. http://proxy.example.com:8080)
    --version               Show the version of the cos
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
  --use_32bit)
    USE_32BIT=1
    shift # past argument
    ;;
  --skip_verify_cert)
    SKIP_VERIFY_CERT=1
    shift # past argument
    ;;
  --install_colistener)
    INSTALL_COLISTENER=1
    shift # past argument
    ;;
  --install_cobridge)
    INSTALL_COBRIDGE=1
    shift # past argument
    ;;
  --enable_master)
    ENABLE_MASTER=1
    shift # past argument
    ;;
  --http_proxy=*)
    HTTP_PROXY="${1#*=}"
    shift # past argument=value
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

if [[ $USE_32BIT -eq 1 ]]; then
  if [[ $ARCH != "arm64" ]] && [[ $ARCH != "arm" ]]; then
    echo_error "32-bit version is only supported on arm64 and arm architecture."
    exit 1
  fi
  ARCH="arm"
  ROS_NODE_ARCH="armhf"
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
echo "server_url:      ${SERVER_URL}"
echo "org_slug:        ${ORG_SLUG}"
echo "project_slug:    ${PROJECT_SLUG}"
echo "coLink_endpoint: ${COLINK_ENDPOINT}"
echo "sn_file:         ${SN_FILE}"
echo "sn_field:        ${SN_FIELD}"
echo "serial_num:      ${SERIAL_NUM}"

# check org_slug and project_slug
if [[ -z "$ORG_SLUG" ]]; then
  # ORG_SLUG is mandatory for all operations as per the new requirements.
  echo_error "ERROR: --org_slug must be provided. Exiting." >&2
  exit 1
fi

if [[ -n "$PROJECT_SLUG" ]]; then
  # PROJECT_SLUG is provided along with ORG_SLUG.
  echo_info "INFO: Using organization '$ORG_SLUG' and project '$PROJECT_SLUG'."
  echo_info "INFO: If project '$PROJECT_SLUG' does not exist under organization '$ORG_SLUG', the device will be installed to the organization, and a warning may be issued by the coScout service."
  PROJECT_SLUG=$ORG_SLUG/$PROJECT_SLUG
  ORG_SLUG=
else
  # Only ORG_SLUG is provided. PROJECT_SLUG is empty.
  echo_info "INFO: Using organization '$ORG_SLUG'. Device will be installed to the organization by default."
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

if [[ $REMOVE_CONFIG -eq 1 ]]; then
  echo "remove exists config file."
  [ -f "/etc/virmesh.key" ] && sudo rm -f /etc/virmesh.key
  [ -f "/etc/virmesh.pub" ] && sudo rm -f /etc/virmesh.pub
  [ -f "/etc/colink.key" ] && sudo rm -f /etc/colink.key
  [ -f "/etc/colink.pub" ] && sudo rm -f /etc/colink.pub
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
    cp "$TEMP_DIR/cos_binaries/trzsz_tar/trzsz_${TRZSZ_VERSION}_linux_${COLINK_ARCH}.tar.gz" "$TEMP_DIR"/trzsz.tar.gz
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
  # check systemd service
  if [[ $DISABLE_SERVICE -eq 0 ]]; then
    if check_systemd; then
      echo "Installing systemd service..."
      sudo tee /etc/systemd/system/colink.service >/dev/null <<EOF
[Unit]
Description=coLink Client Daemon

[Service]
WorkingDirectory=/etc
ExecStart=/usr/local/bin/colink daemon --endpoint ${COLINK_ENDPOINT} --network ${COLINK_NETWORK} --allow-ssh
Restart=always
RestartSec=30

[Install]
WantedBy=multi-user.target
EOF
      sudo systemctl daemon-reload

      echo "Starting coLink service..."
      sudo systemctl is-active --quiet coLink && sudo systemctl stop coLink && sudo systemctl disable coLink && sudo rm -f /etc/systemd/system/coLink.service
      sudo systemctl is-active --quiet virmesh && sudo systemctl stop virmesh && sudo systemctl disable virmesh && sudo rm -f /etc/systemd/system/virmesh.service

      sudo systemctl is-active --quiet colink && sudo systemctl stop colink && sudo systemctl disable colink
      sudo systemctl enable colink
      sudo systemctl start colink
      echo "Start coLink service done."
    else
      echo_error "This script requires systemd. For upstart systems, please use install-initd.sh instead."
      exit 1
    fi
  else
    echo "Skipping systemd service installation, just install coLink binary..."
    echo "You can manually start coLink with the command: /usr/local/bin/colink daemon --endpoint ${COLINK_ENDPOINT} --network ${COLINK_NETWORK} --allow-ssh"
  fi
  echo_info "Successfully installed coLink."
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

# region config
COS_SHELL_BASE="$CUR_USER_HOME/.local"

# make some directories
COS_CONFIG_DIR="$CUR_USER_HOME/.config/cos"
COS_STATE_DIR="$CUR_USER_HOME/.local/state/cos"
COS_LOG_DIR="$CUR_USER_HOME/.local/state/cos/logs"
sudo -u "$CUR_USER" mkdir -p "$COS_CONFIG_DIR" "$COS_STATE_DIR" "$COS_SHELL_BASE/bin" "$COS_LOG_DIR"

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

MASTER_ENABLED=false
if [[ $ENABLE_MASTER -eq 1 ]]; then
  MASTER_ENABLED=true
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

master_slave:
  enabled: $MASTER_ENABLED

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
  TMP_FILE="$TEMP_DIR/cos_binaries/cos/$ARCH/$OS-$ARCH.gz"
  JSON_FILE="$TEMP_DIR/cos_binaries/cos/$ARCH/$OS-$ARCH.json"
  if [[ ! -f $TMP_FILE || ! -f $JSON_FILE ]]; then
    echo "ERROR: Failed to find cos binary or JSON file. Exiting."
    exit 1
  fi
  mv "$TEMP_DIR/cos_binaries/version.yaml" "$COS_STATE_DIR/version.yaml"
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
  if check_systemd; then
    echo "Installing cos systemd service..."

    # Create system-level systemd service file
    echo "Creating cos.service systemd file..."

    # Prepare environment section for systemd service
    ENV_SECTION=""
    if [[ -n "$HTTP_PROXY" ]]; then
      echo "Setting HTTP proxy: $HTTP_PROXY"
      echo "Setting NO_PROXY (default): $NO_PROXY"
      ENV_SECTION="Environment=HTTP_PROXY=$HTTP_PROXY
Environment=HTTPS_PROXY=$HTTP_PROXY
Environment=http_proxy=$HTTP_PROXY
Environment=https_proxy=$HTTP_PROXY
Environment=NO_PROXY=$NO_PROXY
Environment=no_proxy=$NO_PROXY"
    fi

    sudo tee /etc/systemd/system/cos.service >/dev/null <<EOL
[Unit]
Description=coScout: Data Collector by coScene
Documentation=https://github.com/coscene-io/coScout
StartLimitBurst=10
StartLimitIntervalSec=86400

[Service]
Type=simple
User=$CUR_USER
Group=$CUR_USER
WorkingDirectory=$CUR_USER_HOME/.local/state/cos
CPUQuota=10%
${ENV_SECTION}
ExecStart=$COS_SHELL_BASE/bin/cos daemon --config-path=${COS_CONFIG_DIR}/config.yaml --log-dir=${COS_LOG_DIR}
SyslogIdentifier=cos
RestartSec=60
Restart=always

[Install]
WantedBy=multi-user.target
EOL
    echo "Created cos.service systemd file: /etc/systemd/system/cos.service"

    echo "Enabling linger for $CUR_USER..."
    sudo loginctl enable-linger "$CUR_USER"
    XDG_RUNTIME_DIR="/run/user/$(id -u "${CUR_USER}")"
    echo "Setting XDG_RUNTIME_DIR to $XDG_RUNTIME_DIR for user $CUR_USER"
    sudo -u "$CUR_USER" XDG_RUNTIME_DIR="$XDG_RUNTIME_DIR" systemctl --user is-active --quiet cos && sudo -u "$CUR_USER" XDG_RUNTIME_DIR="$XDG_RUNTIME_DIR" systemctl --user stop cos && sudo -u "$CUR_USER" XDG_RUNTIME_DIR="$XDG_RUNTIME_DIR" systemctl --user disable cos

    echo ""
    echo "Starting cos service for $CUR_USER..."

    echo "Reloading systemd daemon..."
    sudo systemctl daemon-reload

    echo "Checking if cos service is running..."
    sudo systemctl is-active --quiet cos && sudo systemctl stop cos && sudo systemctl disable cos

    echo "Enabling cos service..."
    sudo systemctl enable cos

    echo "Starting cos service..."
    if sudo systemctl start cos; then
      echo "Cos service started successfully."
    else
      echo_error "Cos service failed to start."

      echo_error "Checking service status for more details..."
      sudo systemctl status cos || true
      echo_error "Checking journal logs for more details..."
      sudo journalctl -xe -u cos --no-pager | tail -n 50
    fi

    echo ""
    echo_info "🎉 Installation completed successfully , you can use 'tail -f ${COS_LOG_DIR}/cos.log' to check the logs."
  else
    echo_error "This script requires systemd. For upstart systems, please use install-initd.sh instead."
    exit 1
  fi
else
  echo "Skipping systemd service installation, just install cos binary..."
  echo "You can manually start cos with the command: $COS_SHELL_BASE/bin/cos daemon --config-path=${COS_CONFIG_DIR}/config.yaml --log-dir=${COS_LOG_DIR}"
fi

get_ubuntu_distro() {
  # in keenon's ubuntu14.04, /etc/os-release file exists, but `VERSION_CODENAME` and `UBUNTU_CODENAME` not found.
  # so, check /etc/lsb-release first, if file not exists, fallback to /etc/os-release.
  if [[ -f /etc/lsb-release ]]; then
    source /etc/lsb-release
    echo "${DISTRIB_CODENAME:-unknown}"
  elif [[ -f /etc/os-release ]]; then
    source /etc/os-release
    if [[ -n "${VERSION_CODENAME:-}" ]]; then
      echo "$VERSION_CODENAME"
    elif [[ -n "${UBUNTU_CODENAME:-}" ]]; then
      echo "$UBUNTU_CODENAME"
    else
      echo "unknown"
    fi
  else
    echo "unknown"
  fi
}

get_ros_distro() {
  if [[ -n "${ROS_DISTRO:-}" ]]; then
      echo "$ROS_DISTRO"
  else
      # Try to find ROS installation in /opt/ros
      for ros_path in /opt/ros/*; do
          if [[ -d "$ros_path" ]]; then
              echo "$(basename "$ros_path")"
              return 0
          fi
      done
      echo "unknown"
  fi
}

if [[ $INSTALL_COBRIDGE -eq 1 ]] || [[ $INSTALL_COLISTENER -eq 1 ]]; then
  UBUNTU_DISTRO=$(get_ubuntu_distro)
  ROS_VERSION=$(get_ros_distro)
  echo "current ubuntu distro: ${UBUNTU_DISTRO}, ROS distro: ${ROS_VERSION}"
fi

COLISTENER_VERSION="none"
COBRIDGE_VERSION="none"

if [[ $INSTALL_COLISTENER -eq 1 ]]; then
  echo "Install coListener"
  if [[ -n $USE_LOCAL ]]; then
    COLISTENER_DEB_FILE="ros-${ROS_VERSION}-colistener_${UBUNTU_DISTRO}_${ROS_NODE_ARCH}.deb"
    sudo dpkg -i "$TEMP_DIR/cos_binaries/colistener/${UBUNTU_DISTRO}/${ROS_NODE_ARCH}/${ROS_VERSION}/${COLISTENER_DEB_FILE}"
  else
    COLISTENER_VERSION="2.2.0-0"
    COLISTENER_DEB_FILE="ros-${ROS_VERSION}-colistener_${COLISTENER_VERSION}${UBUNTU_DISTRO}_${ROS_NODE_ARCH}.deb"
    COLISTENER_DOWNLOAD_URL="${APT_BASE_URL}/dists/${UBUNTU_DISTRO}/main/binary-${ROS_NODE_ARCH}/${COLISTENER_DEB_FILE}"
    download_file "$TEMP_DIR"/colistener.deb $COLISTENER_DOWNLOAD_URL $SKIP_VERIFY_CERT
    sudo dpkg -i "$TEMP_DIR"/colistener.deb
  fi
fi

if [[ $INSTALL_COBRIDGE -eq 1 ]]; then
  echo "Install coBridge"

  if [[ -n $USE_LOCAL ]]; then
    COBRIDGE_DEB_FILE="ros-${ROS_VERSION}-cobridge_${UBUNTU_DISTRO}_${ROS_NODE_ARCH}.deb"
    sudo dpkg -i "$TEMP_DIR/cos_binaries/cobridge/${UBUNTU_DISTRO}/${ROS_NODE_ARCH}/${ROS_VERSION}/${COBRIDGE_DEB_FILE}"
  else
    COBRIDGE_VERSION="1.1.2-0"
    COBRIDGE_DEB_FILE="ros-${ROS_VERSION}-cobridge_${COBRIDGE_VERSION}${UBUNTU_DISTRO}_${ROS_NODE_ARCH}.deb"
    COBRIDGE_DOWNLOAD_URL="${APT_BASE_URL}/dists/${UBUNTU_DISTRO}/main/binary-${ROS_NODE_ARCH}/${COBRIDGE_DEB_FILE}"
    download_file "$TEMP_DIR"/cobridge.deb $COBRIDGE_DOWNLOAD_URL $SKIP_VERIFY_CERT
    sudo dpkg -i "$TEMP_DIR"/cobridge.deb
  fi
fi

VERSION_FILE="$COS_STATE_DIR/version.yaml"
sudo tee "${VERSION_FILE}" > /dev/null << EOF
# coScene Edge Software Package Versions
# Generated on: $(date -u +"%Y-%m-%d %H:%M:%S UTC")

release_version: online
assemblies:
  colink_version: ${COLINK_VERSION}
  cos_version: ${VERSION}
  colistener_version: ${COLISTENER_VERSION}
  cobridge_version: ${COBRIDGE_VERSION}
  trzsz_version: ${TRZSZ_VERSION}
EOF

echo_info "Successfully installed cos."
exit 0
