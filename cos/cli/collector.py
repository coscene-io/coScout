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
import logging.config
import shutil
import signal
import sys
import threading
import time
from multiprocessing import Process, Queue

import click
import psutil

from cos.cli.context import Context
from cos.collector import Collector, EventCodeManager, CollectorConfig
from cos.collector.collector import DeviceConfig
from cos.collector.mod import Mod
from cos.collector.openers import CosHandler
from cos.config import AppConfig, load_kebab_source, HttpServerConfig
from cos.constant import COS_ONEFILE_PATH
from cos.core.api import ApiClient, ApiClientState, get_client
from cos.core.exceptions import DeviceNotFound, Unauthorized
from cos.core.heartbeat import Heartbeat, HeartbeatConfig
from cos.core.register import Register
from cos.core.server import CustomHttpServer
from cos.install.updater import Updater
from cos.mods import ModLoader
from cos.version import get_version

_log = logging.getLogger(__name__)


# noinspection PyBroadException
def run_forever(config_file: str, conf: AppConfig, cos_url_handler: CosHandler, network_queue: Queue, error_queue: Queue):
    def signal_handler(sig, _):
        print(f"\nProgram exiting gracefully by {sig}")
        sys.exit(0)

    signal.signal(signal.SIGINT, signal_handler)
    is_first_run = True

    cos_url_handler.set_api_client(None)
    load_mod(None, conf)
    while True:
        try:
            if conf.logging:
                logging.config.dictConfig(conf.logging)
            Register(conf.api, conf.device_register).run()

            # check upgrade after authorized
            Updater(conf.updater).run()

            api_client = get_client(conf.api)
            cos_url_handler.set_api_client(api_client=api_client)
            if is_first_run:
                source = load_kebab_source(config_file, extra_url_handler=cos_url_handler)
                source.reload(
                    reload_interval_in_secs=conf.collector.scan_interval_in_secs,
                    skip_first=False,
                )
                conf = source.get(expected_type=AppConfig, update_after_reload=True)
                is_first_run = False

            mod = load_mod(api_client, conf)
            code_manager = EventCodeManager(
                conf=conf.event_code,
                convert_code=mod.convert_code,
                api_client=api_client,
            )
            start_collector_listener(
                conf=conf.collector,
                device_conf=conf.device,
                api_client=api_client,
                code_manager=code_manager,
                network_queue=network_queue,
                error_queue=error_queue,
            )

            start_http_server(
                conf=conf.http_server,
                api_client=api_client,
            )

            mod.run()
        except DeviceNotFound:
            _log.warning("No device found, check if robot.yaml is present waiting for next scan.")
            error_queue.put({"code": "DeviceNotFound"})
        except Unauthorized:
            _log.error(
                "Unauthorized, please check your device authorization status.",
                exc_info=True,
            )
            state = ApiClientState().load_state()
            state.authorized_device(0, "")
            state.save_state()
        except Exception as e:
            # 打印错误，但保证循环不被打断
            _log.error("An error occurred when running collector", exc_info=True)
            error_queue.put({"code": type(e).__name__, "error_msg": str(e)})
        time.sleep(conf.collector.scan_interval_in_secs)


def start_http_server(conf: HttpServerConfig, api_client: ApiClient):
    thread_name = "cos-main-http-server-thread"
    http_server_thread_flag = False

    for t in threading.enumerate():
        if t.name == thread_name:
            http_server_thread_flag = True

    if not http_server_thread_flag:
        _http_server = CustomHttpServer(conf=conf, api_client=api_client)
        t = threading.Thread(
            target=_http_server.start,
            name=thread_name,
            daemon=True,
        )
        t.start()
        _log.info("Thread start run http server")
    else:
        _log.info("Thread already start run http server, skip!")


def start_collector_listener(
    conf: CollectorConfig,
    device_conf: DeviceConfig,
    api_client: ApiClient,
    code_manager: EventCodeManager,
    network_queue: Queue,
    error_queue: Queue,
):
    thread_name = "cos-main-collector-thread"
    collector_thread_flag = False

    for t in threading.enumerate():
        if t.name == thread_name:
            collector_thread_flag = True

    if not collector_thread_flag:
        _collector = Collector(conf=conf, device_conf=device_conf, api_client=api_client, code_manager=code_manager)
        t = threading.Thread(
            target=_collector.run,
            args=(network_queue, error_queue),
            name=thread_name,
            daemon=True,
        )
        t.start()
        _log.info("Thread start run collector")
    else:
        _log.info("Thread already start run collector, skip!")


def load_mod(api_client: ApiClient | None, conf: AppConfig):
    ModLoader.load()

    mod_conf = conf.mod
    mod_name = mod_conf.name.lower()

    _log.info(f"Use mod {mod_name} for collector.")
    return Mod.get_mod(mod_name)(api_client=api_client, conf={**mod_conf.conf, "topics": conf.topics})


@click.command
@click.pass_obj
def daemon(ctx: Context):
    ctx.source.disable_reload()
    # clean_old_binary()
    _log.info(f"Starting collector daemon with {get_version()}")

    network_queue = Queue()
    error_queue = Queue()
    handle = Process(target=run_forever, args=(ctx.config_file, ctx.conf, ctx.cos_url_handler, network_queue, error_queue))

    heart = Heartbeat(api_conf=ctx.conf.api, conf=HeartbeatConfig(), network_queue=network_queue, error_queue=error_queue)
    monitor = Process(target=heart.heartbeat, args=())

    monitor.start()
    handle.start()
    handle.join()
    monitor.join()


def clean_old_binary():
    current_process = psutil.Process()
    children = current_process.children(recursive=True)
    parent = current_process.ppid()
    pids = [current_process.pid] + [c.pid for c in children] + [parent]

    _log.info(f"Daemon started with pid and children pids: {pids}")
    _log.info(f"Clean old binary in {COS_ONEFILE_PATH}")
    if not COS_ONEFILE_PATH.exists():
        return
    for f in COS_ONEFILE_PATH.iterdir():
        _log.info(f"Found binary {f.name}")
        if not f.is_dir():
            continue

        should_keep = any([f"_{pid}_" in str(f.name).lower() for pid in pids])
        if not should_keep:
            shutil.rmtree(str(f.absolute()))
            _log.info(f"Cleaned old binary {f.name}")
