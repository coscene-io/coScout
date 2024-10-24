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
import fnmatch
import json
import logging
import os
import platform
import random
import shutil
import threading
import time
import uuid
from datetime import datetime, timedelta
from functools import partial
from pathlib import Path

from pydantic import BaseModel
from strictyaml import load

from cos.collector import Mod
from cos.constant import COS_DEFAULT_CONFIG_PATH, DEFAULT_MOD_STATE_DIR, DEFAULT_MOD_TEMP_DIR
from cos.core.api import ApiClient
from cos.core.exceptions import DeviceNotFound
from cos.core.models import FileInfo, RecordCache
from cos.mods.common.default.file_state_handler import FileStateHandler
from cos.mods.common.default.handlers import LogHandler
from cos.mods.common.default.remote_rule import RemoteRule
from cos.mods.common.task.task_handler import TaskHandler
from cos.utils import flatten

_log = logging.getLogger(__name__)


class DefaultModConfig(BaseModel):
    enabled: bool = False
    base_dirs: list[str] = []  # Deprecated
    base_dir: str = ""  # Deprecated
    listen_dirs: list[str] = []
    collect_dirs: list[str] = []
    topics: list[str] = []
    sn_file: str | None = ""
    sn_field: str | None = ""
    ros2_customized_msgs_dirs: list[str] = []
    upload_files: list[str] = []


class DefaultMod(Mod):
    def __init__(self, api_client: ApiClient, conf: dict = None):
        if not conf:
            _conf = DefaultModConfig()
        else:
            _conf = DefaultModConfig.model_validate(conf)

        self._api_client = api_client
        self.conf = _conf
        self.log_thread_name = "cos-mod-default-log-listener"
        self.task_thread_name = "cos-mod-task-collector-handler"
        self.static_file_thread_name = "cos-mod-default-static-file-listener"
        self.file_state_handler = FileStateHandler.get_instance(self.conf.ros2_customized_msgs_dirs)

        self.state_dir = DEFAULT_MOD_STATE_DIR
        self.state_dir.mkdir(parents=True, exist_ok=True)
        self.temp_dir = DEFAULT_MOD_TEMP_DIR
        self.temp_dir.mkdir(parents=True, exist_ok=True)
        super().__init__()

    @staticmethod
    def __find_error_json(target_dir: Path):
        for file_path in target_dir.glob("**/*.json"):
            yield str(file_path)

    @staticmethod
    def __update_error_json(error_json, error_json_path):
        with open(error_json_path, "w", encoding="utf8") as fp:
            json.dump(error_json, fp, indent=4)

    def handle_error_json(self, error_json_path: str):
        with open(error_json_path, "r", encoding="utf8") as fp:
            error_json = json.load(fp)

        # 如果 flag（文件已经找齐）为 True 并且还未 uploaded.
        if "flag" in error_json and error_json["flag"] and "uploaded" not in error_json and "skipped" not in error_json:
            source_file = Path(error_json_path)
            start_time = error_json.get("startTime")
            rc = RecordCache(
                timestamp=int(start_time),
            ).load_state()
            if error_json.get("projectName"):
                rc.project_name = error_json.get("projectName")

            target_file = Path(rc.base_dir_path) / source_file.name
            if not target_file.exists():
                target_file.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy(source_file, target_file)
                _log.info(f"==> Copy error json file to record folder: {target_file}")

            files = {str(target_file): FileInfo(filepath=str(target_file), filename=target_file.name)}
            for key in ["bag", "log", "files"]:
                for filepath in error_json.get(key, []):
                    filename = key + "/" + Path(filepath).name
                    files[filename] = FileInfo(filepath=filepath, filename=filename)
            for key in ["zips"]:
                for filepath in error_json.get(key, []):
                    filename = Path(filepath).name
                    files[filename] = FileInfo(filepath=filepath, filename=filename)
            for dir_base_path in error_json.get("dirs", []):
                for filepath in Path(dir_base_path).glob("**/*"):
                    if filepath.is_file():
                        filename = str(filepath.relative_to(Path(dir_base_path).parent))
                        files[filename] = FileInfo(filepath=filepath, filename=filename)

            rc.file_infos = list(files.values())
            rc.record = {
                "title": error_json.get("record", {}).get("title", "Device Auto Upload - " + str(rc.timestamp)),
                "description": error_json.get("record", {}).get("description", "Device Auto Upload"),
                "rules": error_json.get("record", {}).get("rules", []),
            }
            rc.labels = error_json.get("record", {}).get("labels", [])
            rc.paths_to_delete = error_json.get("paths_to_delete", [])

            # update diagnosis task
            rc.diagnosis_task = error_json.get("diagnosis_task", {})

            rc.save_state()
            _log.info(f"==> Converted error log to record state: {rc.state_path}")

            # 把上传状态写回json
            error_json["uploaded"] = True
            self.__update_error_json(error_json, error_json_path)
            _log.info(f"==> Handle err file done: {error_json_path}")
        else:
            _log.debug(f"==> Skip handle err file: {error_json_path}")

    def __find_files_and_update_error_json(self, error_json_path: str):
        with open(error_json_path, "r", encoding="utf8") as fp:
            error_json = json.load(fp)

        if "flag" not in error_json or error_json["flag"] or "cut" not in error_json:
            return

        if datetime.now().timestamp() < error_json["cut"]["end"]:
            return

        start_time = error_json["cut"]["start"]
        end_time = error_json["cut"]["end"]
        error_json_id, _ = os.path.splitext(os.path.basename(error_json_path))
        temp_files_dir = self.temp_dir / error_json_id
        temp_files_dir.mkdir(parents=True, exist_ok=True)

        _log.info(
            f"==> Search for files in {self.file_state_handler.src_dirs}, start_time: {start_time}, end_time: {end_time}"
        )

        def upload_whitelist_filter(filename, _file_state):
            if not error_json["cut"]["whiteList"]:
                # return True if whiteList is empty or None
                return True
            return any(fnmatch.fnmatchcase(filename, pattern) for pattern in error_json["cut"]["whiteList"])

        raw_files = self.file_state_handler.get_files(
            FileStateHandler.state_is_collecting_filter(),
            FileStateHandler.state_timestamp_filter(start_time, end_time),
            FileStateHandler.state_dir_filter(False),
            upload_whitelist_filter,
        )
        _log.info(f"==> Found files: {raw_files}")
        raw_files += error_json["cut"]["extraFiles"]

        _log.info(f"==> Search for dirs in {self.file_state_handler.src_dirs}, start_time: {start_time}, end_time: {end_time}")
        raw_dirs = self.file_state_handler.get_files(
            FileStateHandler.state_is_collecting_filter(),
            FileStateHandler.state_timestamp_filter(start_time, end_time),
            FileStateHandler.state_dir_filter(True),
            upload_whitelist_filter,
        )
        _log.info(f"==> Found dirs: {raw_dirs}")

        bag_files = []
        log_files = []
        other_files = []
        dirs = []
        zips = []
        for dir_name in raw_dirs:
            dir_path = Path(dir_name)
            cur_dir = temp_files_dir / dir_path.name
            shutil.copytree(dir_path, cur_dir)
            dirs.append(str(cur_dir))
        for file in raw_files:
            # todo: use handlers to handle different file types
            try:
                if Path(file).is_file():
                    if file.endswith(".bag"):
                        dst_path = shutil.copy(file, temp_files_dir)
                        bag_files.append(dst_path)
                    elif file.endswith(".log"):
                        dst_path = LogHandler.prepare_cut(Path(file), temp_files_dir, start_time, end_time)
                        log_files.append(dst_path)
                    else:
                        dst_path = shutil.copy(file, temp_files_dir)
                        other_files.append(dst_path)
                elif Path(file).is_dir():
                    dst_path = (temp_files_dir / Path(file).name).with_suffix(".zip")
                    shutil.make_archive(
                        str(dst_path.with_suffix("")),
                        "zip",
                        Path(file).parent,
                        Path(file).name,
                    )
                    zips.append(str(dst_path))
            except Exception:
                _log.error(f"==> Cut file failed: {file}", exc_info=True)

        error_json["bag"] = bag_files
        error_json["log"] = log_files
        error_json["files"] = other_files
        error_json["dirs"] = dirs
        error_json["zips"] = zips
        error_json["flag"] = True
        error_json["startTime"] = int(time.time() * 1000) + random.randint(1, 1000)
        error_json["paths_to_delete"] = [str(temp_files_dir)]

        with open(error_json_path, "w", encoding="utf8") as fp:
            json.dump(error_json, fp, indent=4)

    @staticmethod
    def __upload_impl(
        before,
        title,
        description,
        labels,
        extra_files,
        state_dir: Path,
        project_name,
        trigger_ts,
        rule,
        white_list,
        after=0,
    ):
        assert before >= 0 or after >= 0, "before or after must be greater than 0"
        start_time_raw = datetime.fromtimestamp(trigger_ts) - timedelta(minutes=before)
        end_time_raw = datetime.fromtimestamp(trigger_ts) + timedelta(minutes=after)
        start_time = int(start_time_raw.timestamp())
        end_time = int(end_time_raw.timestamp())

        json_path = state_dir / f"{uuid.uuid4()}.json"
        json_path.parent.mkdir(parents=True, exist_ok=True)

        upload_data = {
            "flag": False,
            "projectName": project_name,
            "record": {},
            "diagnosis_task": {},
            "cut": {
                "extraFiles": extra_files,
                "start": start_time,
                "end": end_time,
                "whiteList": white_list,
            },
        }
        if title:
            upload_data["record"]["title"] = title
        if description:
            upload_data["record"]["description"] = description
        if labels:
            upload_data["record"]["labels"] = labels
        if rule:
            upload_data["record"]["rules"] = [{"id": rule.get("id", "")}]
            upload_data["diagnosis_task"]["rule_id"] = rule.get("id", "")
            upload_data["diagnosis_task"]["rule_name"] = rule.get("name", "")
        upload_data["diagnosis_task"]["trigger_time"] = trigger_ts
        upload_data["diagnosis_task"]["start_time"] = start_time
        upload_data["diagnosis_task"]["end_time"] = end_time

        DefaultMod.__update_error_json(upload_data, json_path)

    @staticmethod
    def __handle_unprocessed_files(
        api_client: ApiClient,
        file_state_handler: FileStateHandler,
        upload_fn: partial,
    ):
        _log.info(f"==> Search for files in {file_state_handler.src_dirs}")
        for file in file_state_handler.get_files(FileStateHandler.state_is_listening_filter()):
            file_state_handler.static_file_diagnosis(
                api_client,
                Path(file),
                upload_fn,
                file_state_handler.active_topics,
            )

    def run(self):
        if not self.conf.enabled:
            _log.info("Default Mod is not enabled, skip!")
            return

        self.start_task_handler(self._api_client, self.conf.upload_files)

        # Compute the listening directories, use base_dirs and base_dir if listen_dirs is not set or empty
        base_dirs = {Path(base_dir).absolute() for base_dir in self.conf.base_dirs}
        if self.conf.base_dir:
            base_dirs.add(Path(self.conf.base_dir).absolute())
        listen_dirs = {Path(listen_dir).absolute() for listen_dir in self.conf.listen_dirs}
        collect_dirs = {Path(collect_dir).absolute() for collect_dir in self.conf.collect_dirs}
        if not listen_dirs:
            listen_dirs = base_dirs
        if not collect_dirs:
            collect_dirs = base_dirs

        if not listen_dirs:
            _log.info("Default Mod listen_dirs/base_dirs/base_dir is empty, skip!")
            return

        self.file_state_handler.update_dirs(listen_dirs, collect_dirs)

        # Compute topics in both rules and config
        self.file_state_handler.active_topics = {
            topic
            for topic in RemoteRule(self._api_client).list_topics_in_rules()
            if topic in [*self.conf.topics, "/external_log"]
        }

        # start log listener and static file listener
        self.start_log_listener()
        self.start_static_file_listener()

        # handle error json files
        _log.info(f"==> Search for new error json {str(self.state_dir)}")

        self.file_state_handler.update_dirs(listen_dirs, collect_dirs)  # update dirs before find error json
        for error_json_path in self.__find_error_json(self.state_dir):
            # noinspection PyBroadException
            try:
                self.__find_files_and_update_error_json(error_json_path)
                self.handle_error_json(error_json_path)
            except Exception:
                # 打印错误，但保证循环不被打断
                _log.error(f"An error occurred when handling: {error_json_path}", exc_info=True)

    def start_log_listener(self):
        log_thread_flag = False

        for t in threading.enumerate():
            if t.name == self.log_thread_name:
                log_thread_flag = True

        if not log_thread_flag:
            t = threading.Thread(
                target=LogHandler().scan_dirs_and_diagnose,
                args=(
                    self._api_client,
                    partial(
                        DefaultMod.__upload_impl,
                        state_dir=self.state_dir,
                    ),
                ),
                name=self.log_thread_name,
                daemon=True,
            )
            t.start()
            _log.info("Thread start log listener")
        else:
            _log.info("Thread already start log listener, skip!")

    def start_static_file_listener(self):
        static_file_thread_flag = False

        for t in threading.enumerate():
            if t.name == self.static_file_thread_name:
                static_file_thread_flag = True

        if not static_file_thread_flag:
            t = threading.Thread(
                target=DefaultMod.__handle_unprocessed_files,
                args=(
                    self._api_client,
                    self.file_state_handler,
                    partial(DefaultMod.__upload_impl, state_dir=self.state_dir),
                ),
                name=self.static_file_thread_name,
                daemon=True,
            )
            t.start()
            _log.info("Thread start static file listener")
        else:
            _log.info("Thread already start static file listener, skip!")

    def start_task_handler(self, api_client: ApiClient, upload_files: list[str]):
        if upload_files is None:
            upload_files = []

        task_thread_flag = False
        for t in threading.enumerate():
            if t.name == self.task_thread_name:
                task_thread_flag = True

        if not task_thread_flag:
            t = threading.Thread(
                target=TaskHandler(api_client, upload_files).run,
                args=(),
                name=self.task_thread_name,
                daemon=True,
            )
            t.start()
            _log.info("Thread start handle task")
        else:
            _log.info("Thread task already handle, skip!")

    @staticmethod
    def __generate_device_sn():
        sn_file_path = COS_DEFAULT_CONFIG_PATH.parent / "sn.txt"
        sn_file_path.parent.mkdir(parents=True, exist_ok=True)
        try:
            if not sn_file_path.is_file():
                sn = uuid.uuid4().hex
                with open(sn_file_path.absolute(), "w", encoding="utf8") as y:
                    y.write(sn)
        except PermissionError as e:
            raise DeviceNotFound(f"access to {sn_file_path} denied") from e

        with open(sn_file_path.absolute(), "r", encoding="utf8") as y:
            sn = y.read().strip()

        node = platform.node()
        return {
            "serial_number": sn,
            "display_name": f"{node}@{sn}",
            "description": f"node: {node}, sn: {sn}",
        }

    def get_device(self) -> dict[str, str]:
        if not self.conf.sn_file:
            return self.__generate_device_sn()

        sn_file_path = Path(self.conf.sn_file)
        if not sn_file_path.exists():
            return self.__generate_device_sn()

        file_path_str = str(sn_file_path.absolute())
        if file_path_str.endswith(".txt"):
            with open(sn_file_path, "r", encoding="utf8") as y:
                sn = y.read().strip()
            return {
                "serial_number": sn,
                "display_name": sn,
                "description": sn,
            }
        elif self.conf.sn_field and (
            file_path_str.endswith(".json") or file_path_str.endswith(".yaml") or file_path_str.endswith(".yml")
        ):
            with open(sn_file_path, "r", encoding="utf8") as y:
                try:
                    _data = load(y.read()).data
                    flatten_data = flatten(_data)
                except Exception:
                    _log.error("Failed to load sn file", exc_info=True)
                    return self.__generate_device_sn()
            sn = flatten_data.get(self.conf.sn_field)
            if not sn:
                _log.error("Failed to get sn field", exc_info=True)
                raise DeviceNotFound(f"Failed to get sn field from {sn_file_path}")
            return {
                "serial_number": sn,
                "display_name": sn,
                "description": sn,
            }
        return self.__generate_device_sn()

    def convert_code(self, code_json):
        code_list = []
        if isinstance(code_json, list):
            code_list = code_json
        elif isinstance(code_json, dict):
            code_list = code_json.get("msg", [])

        return {str(item.get("code", "")): str(item.get("messageCN", "未知错误")) for item in code_list}

    def find_files(self, trigger_time):
        pass
