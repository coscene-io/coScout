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
cd "$(dirname "${BASH_SOURCE[0]}")/.."

# check temp dir
TEMP_DIR=$(mktemp -d)
if [ ! -e "$TEMP_DIR" ]; then
  echo >&2 "Failed to create temp directory"
  exit 1
fi

COLINK_VERSION="1.0.3"
COS_VERSION="latest"
TRZSZ_VERSION="1.1.6"

SUPPORT_OS=("linux")
SUPPORT_COS_ARCH=("amd64" "arm64" "arm")
SUPPORT_MESH_ARCH=("amd64" "aarch64")

MESH_BASE_URL=https://coscene-download.oss-cn-hangzhou.aliyuncs.com
COS_BASE_URL=https://coscene-download.oss-cn-hangzhou.aliyuncs.com/coscout/v2

# get user input
while test $# -gt 0; do
  case $1 in
  --cos_version=*)
    COS_VERSION="${1#*=}"
    shift # past argument=value
    ;;
  *)
    echo "unknown option: $1"
    help
    exit 1
    ;;
  esac
done

# for range support mesh arch
for arch in "${SUPPORT_MESH_ARCH[@]}"; do
  mesh_folder="${TEMP_DIR}/colink"
  mkdir -p "${mesh_folder}"

  trzsz_folder="${TEMP_DIR}/trzsz_tar"
  mkdir -p "${trzsz_folder}"

  mesh_download_url=${MESH_BASE_URL}/colink/v${COLINK_VERSION}/colink-${arch}
  trzsz_download_url=${MESH_BASE_URL}/trzsz/v${TRZSZ_VERSION}/trzsz_${TRZSZ_VERSION}_linux_${arch}.tar.gz

  echo "Downloading ${mesh_download_url}"
  curl -L -o "${mesh_folder}/colink-${arch}" "${mesh_download_url}"
  curl -L -o "${trzsz_folder}/trzsz_${TRZSZ_VERSION}_linux_${arch}.tar.gz" "${trzsz_download_url}"
done

# for range support cos arch
for os in "${SUPPORT_OS[@]}"; do
  for arch in "${SUPPORT_COS_ARCH[@]}"; do
    cos_folder="${TEMP_DIR}/cos/${arch}"
    mkdir -p "${cos_folder}"

    cos_download_url=${COS_BASE_URL}/${COS_VERSION}/${os}-${arch}.gz
    cos_metadata_url=${COS_BASE_URL}/${COS_VERSION}/${os}-${arch}.json

    echo "Downloading ${cos_download_url}"
    curl -L -o "${cos_folder}/${os}-${arch}.gz" "${cos_download_url}"
    curl -L -o "${cos_folder}/${os}-${arch}.json" "${cos_metadata_url}"

    # if arch is arm, download cgroup-bin
    if [ "${arch}" == "arm" ]; then
      cgroup_bin_download_url=${MESH_BASE_URL}/cgroup_bin/${arch}/cgroup_bin.deb
      curl -L -o "${cos_folder}/cgroup_bin.deb" "${cgroup_bin_download_url}"

      cgroup_lite_download_url=${MESH_BASE_URL}/cgroup_bin/${arch}/cgroup_lite.deb
      curl -L -o "${cos_folder}/cgroup_lite.deb" "${cgroup_lite_download_url}"

      libcgroup_download_url=${MESH_BASE_URL}/cgroup_bin/${arch}/libcgroup1.deb
      curl -L -o "${cos_folder}/libcgroup1.deb" "${libcgroup_download_url}"
    fi
  done
done
# tar temp dir with all binaries
tar -cvzf "${HOME}/cos_binaries.tar.gz" -C "${TEMP_DIR}/" "."

# tar binaries with single arch, if arch is arm64 or x86_64, contains virmesh and trzsz
for arch in "${SUPPORT_COS_ARCH[@]}"; do
  if [ "${arch}" == "arm" ]; then
    tar -cvzf "${HOME}/cos_binaries_${arch}.tar.gz" -C "${TEMP_DIR}/" "cos/${arch}"
  else
    # arch is arm64, set virmesh arch to aarch64
    if [ "${arch}" == "arm64" ]; then
      mesh_arch="aarch64"
    else
      mesh_arch="amd64"
    fi

    tar -cvzf "${HOME}/cos_binaries_${arch}.tar.gz" -C "${TEMP_DIR}/" "cos/${arch}" "colink/colink-${mesh_arch}" "trzsz_tar/trzsz_${TRZSZ_VERSION}_linux_${mesh_arch}.tar.gz"
  fi
done

# ls ${HOME}
cd "${HOME}"
echo "${PWD}"
ls -al "${HOME}"
