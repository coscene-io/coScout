# -*- coding:utf-8 -*-
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

import json
import logging
from functools import partial
from http.server import BaseHTTPRequestHandler, HTTPServer

from cos.config import HttpServerConfig
from cos.core.api import ApiClient
from cos.mods.common.default.remote_rule import RemoteRule

_log = logging.getLogger(__name__)


class SimpleHTTPRequestHandler(BaseHTTPRequestHandler):
    def __init__(self, api_client, *args, **kwargs):
        self._api_client = api_client
        # BaseHTTPRequestHandler calls do_GET **inside** __init__ !!!
        # So we have to call super().__init__ after setting attributes.
        super().__init__(*args, **kwargs)

    def do_GET(self):
        if self.path == "/rules":
            res = self._get_project_rules()

            # response with json, 200 for alive, 500 for not alive
            self.send_response(200)
            self.send_header("Content-type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps(res).encode())
        else:
            # 404 对于未知路由
            self.send_response(404)
            self.end_headers()

    def do_POST(self):
        length = int(self.headers.get("content-length"))
        payload_string = self.rfile.read(length).decode("utf-8")
        payload = json.loads(payload_string) if payload_string else None

        if payload is None:
            self.send_response(400)
            self.end_headers()
            return

        if self.path == "/rules/count":
            res = self._count_rule(payload)
            # response with json, 200 for alive, 500 for not alive
            self.send_response(200)
            self.send_header("Content-type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps(res).encode())
        elif self.path == "/rules/trigger":
            res = self._trigger_rule(payload)
            # response with json, 200 for alive, 500 for not alive
            self.send_response(200)
            self.send_header("Content-type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps(res).encode())
        else:
            # 404 对于未知路由
            self.send_response(404)
            self.end_headers()

    def _get_project_rules(self):
        try:
            rules = RemoteRule(self._api_client).get_project_diagnosis_rules()
            return {"success": True, "data": rules}
        except Exception as e:
            _log.error(f"Failed to get project rules: {e}")
            return {"success": False, "error": str(e)}

    def _count_rule(self, payload):
        try:
            request_device = payload.get("device", None)
            device_name = (
                self._api_client.state.load_state().device.get("name", "") if request_device is None else request_device
            )

            count = self._api_client.count_diagnosis_rules_hit(payload["project"], payload["hit"], device_name)
            return {"success": True, "data": count}
        except Exception as e:
            _log.error(f"Failed to count rule: {e}")
            return {"success": False, "error": str(e)}

    def _trigger_rule(self, payload):
        try:
            request_device = payload.get("device", None)
            device_name = (
                self._api_client.state.load_state().device.get("name", "") if request_device is None else request_device
            )

            self._api_client.hit_diagnosis_rule(payload["rule"], payload["hit"], device_name, payload["triggered"])
            return {"success": True}
        except Exception as e:
            _log.error(f"Failed to trigger rule: {e}")
            return {"success": False, "error": str(e)}


class CustomHttpServer:
    def __init__(self, conf: HttpServerConfig, api_client: ApiClient):
        http_handler = partial(SimpleHTTPRequestHandler, api_client)
        self.server = HTTPServer(("localhost", conf.port), http_handler)

    def start(self):
        self.server.serve_forever()
