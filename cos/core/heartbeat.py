# Copyright 2024 coScene
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

import logging
import signal
import sys
import time
from multiprocessing import Queue

from pydantic import BaseModel

from cos.core import request_hook
from cos.core.api import ApiClientConfig, get_client, ApiClientState
from cos.core.exceptions import Unauthorized
from cos.version import get_version

_log = logging.getLogger(__name__)


class HeartbeatConfig(BaseModel):
    interval_in_secs: int = 60


class Heartbeat:
    def __init__(self, api_conf: ApiClientConfig, conf: HeartbeatConfig, network_queue: Queue, error_queue: Queue):
        self.conf = conf
        self.api_conf = api_conf
        self._network_queue = network_queue
        self._error_queue = error_queue

    def heartbeat(self):
        def signal_handler(sig, _):
            print(f"\nProgram exiting gracefully by {sig}")
            sys.exit(0)

        signal.signal(signal.SIGINT, signal_handler)

        while True:
            try:
                if not ApiClientState().load_state().is_authed():
                    _log.info("device is not authed, skip heartbeat.")
                    time.sleep(self.conf.interval_in_secs)
                    continue

                api_client = get_client(self.api_conf)
                api_state = api_client.state.load_state()
                device = api_state.device
                if not device or not device.get("name"):
                    _log.warning("device name not found, skipping")
                    time.sleep(self.conf.interval_in_secs)
                    continue

                current_version = get_version()
                if current_version is None:
                    current_version = ""

                while not self._network_queue.empty():
                    usage = self._network_queue.get()
                    request_hook.increase_upload_bytes(usage.get("upload", 0))
                    request_hook.increase_download_bytes(usage.get("download", 0))

                error_info = {}
                if self._error_queue.empty():
                    error_info["code"] = "SUCCEEDED"

                if self._error_queue.qsize() > 10:
                    while not self._error_queue.empty():
                        _error = self._error_queue.get()
                        error_info["code"] = _error.get("code", "unknown")
                        error_info["error_msg"] = _error.get("error_msg", "")

                api_client.send_heartbeat(
                    device_name=device["name"],
                    cos_version=current_version,
                    network_usage={
                        "upload_bytes": request_hook.get_network_upload_usage(),
                        "download_bytes": request_hook.get_network_download_usage(),
                    },
                    extra_info=error_info,
                )
                request_hook.reset_network_usage()
            except Unauthorized as e:
                _log.error(f"==> Unauthorized when send device heartbeat", exc_info=True)
                state = ApiClientState().load_state()
                state.authorized_device(0, "")
                state.save_state()
            except Exception:
                _log.error("send device heartbeat error", exc_info=True)

            time.sleep(self.conf.interval_in_secs)
