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
import logging.config
import textwrap
from datetime import datetime
from typing import List
from multiprocessing import Queue

from pydantic import BaseModel, Field
from strictyaml import load, YAMLError

from cos.constant import RECORD_DIR_PATH
from cos.core.api import ApiClient, ApiClientState
from cos.core.models import FileInfo, RecordCache
from cos.utils import is_image
from .codes import EventCodeManager
from ..core import request_hook
from ..core.exceptions import Unauthorized, RecordNotFound
from ..utils.files import can_read_path

_log = logging.getLogger(__name__)


class DeviceConfig(BaseModel):
    extra_files: List[str] = Field(default_factory=list)


class CollectorConfig(BaseModel):
    delete_after_upload: bool = True
    delete_after_interval_in_hours: int = 48
    scan_interval_in_secs: int = 60
    skip_check_same_file: bool = False


class Collector:
    def __init__(
        self,
        conf: CollectorConfig,
        device_conf: DeviceConfig,
        api_client: ApiClient,
        code_manager: EventCodeManager,
    ):
        self.conf = conf
        self.device_conf = device_conf
        self.api = api_client
        self.code_mgr = code_manager
        self.device = self.api.state.load_state().device

    def _upload_record_thumbnail(self, record_name, rec_cache: RecordCache):
        if not record_name:
            return
        for f in rec_cache.file_infos:
            if is_image(f.filename):
                upload_url = self.api.generate_record_thumbnail_upload_url(record_name)
                if upload_url:
                    self.api.upload_file(str(f.filepath), upload_url)
                    return

    def _upload_finish_flag_file(self, record_name, rec_cache: RecordCache) -> bool:
        _finish_file = rec_cache.base_dir_path / "finish.flag"
        if not _finish_file.exists():
            _finish_file.touch()
            with _finish_file.open("w", encoding="utf8") as fp:
                fp.write(json.dumps(rec_cache.files, indent=2))

        file_info = FileInfo(
            filepath=str(_finish_file.absolute()),
            filename=_finish_file.name,
        ).complete(inplace=True, skip_sha256=True)
        return self.api.resumable_upload_files(
            record_name=record_name,
            file_infos=[file_info],
            part_state_path=str(rec_cache.base_dir_path.absolute()),
            skip_check_same_file=self.conf.skip_check_same_file,
        )

    def __get_record_title(self, rec_cache: RecordCache):
        title = rec_cache.record.get("title", None)
        if title:
            return title
        if rec_cache.task.get("title", None):
            return rec_cache.task.get("title")

        code = rec_cache.event_code
        msg = self.code_mgr.get_message(code)
        trigger_time = datetime.fromtimestamp(rec_cache.timestamp / 1000).isoformat()
        return f'{msg or "未知错误"} ({code}) @ {trigger_time}'

    def __make_record_description(self, title, rec_cache: RecordCache):
        description = rec_cache.record.get("description", None)
        if description:
            return description

        result = textwrap.dedent(
            f"""\
            ### {title} \n
            the record is triggered @ {rec_cache.timestamp} \n
            the files are from {rec_cache.base_dir_path} \n
            on robot: {self.device.get("serial_number", "")} \n
        """
        )
        if self.device:
            for label in self.device.get("labels", []):
                result += "\n" + label.get("displayName", "")
        return result

    def _load_device_extra_infos(self):
        infos = {}

        for p in self.device_conf.extra_files:
            if not can_read_path(p):
                continue

            if not (p.endswith(".yml") or p.endswith(".yaml")):
                continue

            with open(p, "r", encoding="utf8") as y:
                try:
                    robot_yaml = load(y.read()).data
                except YAMLError as exc:
                    raise ValueError(f"Failed to load device extra yaml: {exc}")
            # merge robot yaml into infos dict
            infos.update(robot_yaml)
        return infos

    def _create_record(self, rec_cache: RecordCache):
        record_title = self.__get_record_title(rec_cache)
        record = self.api.create_or_get_record(
            file_infos=rec_cache.file_infos,
            title=record_title,
            description=self.__make_record_description(record_title, rec_cache),
            labels=rec_cache.labels,
            device_name=self.device.get("name"),
            record_name=rec_cache.record.get("name") if rec_cache.record else None,
            reserve_file_infos=True,
            rules=rec_cache.record.get("rules", []),
        )
        return record

    def _create_record_related_resources(self, rec_cache: RecordCache):
        record_name = rec_cache.record.get("name", "")
        record_title = rec_cache.record.get("title", "")
        if not record_name:
            return

        for moment in rec_cache.moments:
            if not moment.name:
                # 当前部分组件使用的是毫秒级别的时间戳，所以这里需要转换
                if moment.timestamp > 1_000_000_000_000:
                    moment.timestamp /= 1000

                obtain_event_res = self.api.obtain_event(
                    record_name=record_name,
                    display_name=moment.title if moment.title else record_title,
                    description=moment.description if moment.description else record_title,
                    customized_fields=moment.metadata,
                    trigger_time=moment.timestamp,
                    duration=moment.duration,
                    rule_id=moment.rule_id,
                    device_name=self.device["name"],
                )
                moment.name = obtain_event_res.get("event", {}).get("name")
                moment.is_new = obtain_event_res.get("isNew", False)
                rec_cache.save_state()

            if moment.name and not moment.event:
                has_sent = moment.event.get("sent", False)
                if not has_sent:
                    device_extra_infos = self._load_device_extra_infos()

                    self.api.trigger_device_event(
                        moment=moment,
                        record_name=record_name,
                        device_name=self.device.get("name"),
                        device_extra_info=device_extra_infos,
                        event_code=moment.code,
                        rule_id=moment.rule_id,
                    )
                    moment.event["sent"] = True
                    rec_cache.save_state()
                    _log.info(f"==> Triggered event: {moment.name}")

            if not moment.is_new:
                continue

            if not moment.task:
                continue

            if not moment.task.name:
                upserted_task = self.api.upsert_task(
                    record_name=record_name,
                    event_name=moment.name,
                    title=moment.title if moment.title else record_title,
                    description=moment.description if moment.description else record_title,
                    assignee=moment.task.assignee,
                )

                moment.task.name = upserted_task.get("name")
                rec_cache.save_state()
                if upserted_task.get("name") and moment.task.sync_task:
                    self.api.sync_task(upserted_task.get("name"))

        if rec_cache.diagnosis_task and not rec_cache.diagnosis_task.get("name"):
            created_diagnosis_task = self.api.create_diagnosis_task(
                title=record_title,
                description=self.__make_record_description(record_title, rec_cache),
                device=self.device.get("name"),
                rule_id=rec_cache.diagnosis_task.get("rule_id"),
                rule_name=rec_cache.diagnosis_task.get("rule_name"),
                trigger_time=rec_cache.diagnosis_task.get("trigger_time"),
                start_time=rec_cache.diagnosis_task.get("start_time"),
                end_time=rec_cache.diagnosis_task.get("end_time"),
                record_name=record_name,
            )
            rec_cache.diagnosis_task["name"] = created_diagnosis_task.get("name")
            rec_cache.save_state()

    def handle_record(self, rec_cache: RecordCache):
        _log.debug(f"==> Checking record: {rec_cache.key}")
        # setup project name
        if rec_cache.project_name:
            self.api.project_name = rec_cache.project_name
        else:
            self.api.project_name = None

        # 0. 如果 skipped，直接退出
        if rec_cache.skipped:
            _log.debug(f"==> Record previously skipped: {rec_cache.key}")

        # 1. 如果还没创建过 record, 检查是否超过了 code limit
        elif not rec_cache.record.get("name") and rec_cache.event_code and self.code_mgr.is_over_limit(rec_cache.event_code):
            _log.warning(f"==> Reached code limit {rec_cache.event_code}, skip handle: {rec_cache.key}")
            task_name = rec_cache.task.get("name", "")
            if task_name:
                self.api.update_task_state(task_name, "SUCCEEDED")

            # 把跳过标记写回到json
            rec_cache.skipped = True
            rec_cache.save_state()
            rec_cache.delete_cache_dir(self.conf.delete_after_interval_in_hours)

        else:
            # 如果没有被 collected (文件未找齐).
            if not rec_cache.record.get("name"):
                # 2. 收集文件
                rec_cache.file_infos = [
                    FileInfo(
                        filepath=f.filepath.resolve().absolute(),
                        filename=f.filename,
                    ).complete(inplace=True, skip_sha256=True)
                    for f in rec_cache.file_infos
                    if f.filepath.is_file() and f.filename != "finish.flag" and can_read_path(str(f.filepath.absolute()))
                ]

                # 3. 创建 record 和 event
                rec_cache.record = self._create_record(rec_cache)
                # 为防止断连后重新建立记录，需要立即写回json
                rec_cache.save_state()
                self.code_mgr.hit(rec_cache.event_code)

                task_name = rec_cache.task.get("name", "")
                if task_name:
                    self.api.put_task_tags(task_name, {"recordName": rec_cache.record.get("name", "")})

                # 4. 上传缩略图
                self._upload_record_thumbnail(rec_cache.record.get("name"), rec_cache)

            # 检查关联的资源是否已经上传
            self._create_record_related_resources(rec_cache)

            # 找齐文件后，如果还没有 uploaded (上传完毕).
            if not rec_cache.uploaded:
                _uploaded_files = rec_cache.uploaded_filepaths

                # 5. 等待上传文件清单
                file_infos = []
                for f in rec_cache.file_infos:
                    if can_read_path(str(f.filepath.absolute())):
                        file_infos.append(f)
                    else:
                        _log.warning(f"{f.filepath} can not access, skip!")
                sorted_files: List[FileInfo] = sorted(file_infos, key=lambda f: f.size)

                all_completed = True
                for file_info in sorted_files:
                    _abs_path = str(file_info.filepath.absolute())

                    if not can_read_path(_abs_path):
                        _log.warning(f"{file_info.filepath} can not access, skip!")
                        continue

                    if _abs_path in _uploaded_files:
                        _log.debug(f"{file_info.filepath} already uploaded, skip!")
                        continue

                    if not rec_cache.state_path.absolute().exists():
                        _log.warning(f"{rec_cache.state_path.absolute()} not exist, skip!")
                        return

                    _rc = RecordCache.load_state_from_disk(rec_cache.state_path.absolute())
                    if _rc.skipped:
                        return

                    _completed = self.api.resumable_upload_files(
                        record_name=rec_cache.record["name"],
                        file_infos=[file_info],
                        part_state_path=str(rec_cache.base_dir_path.absolute()),
                        skip_check_same_file=self.conf.skip_check_same_file,
                    )
                    all_completed = all_completed and _completed

                    _uploaded_files.append(_abs_path)
                    rec_cache.uploaded_filepaths = _uploaded_files
                    rec_cache.save_state()

                    task_name = rec_cache.task.get("name", "")
                    if task_name:
                        self.api.put_task_tags(task_name, {"uploadedFiles": str(len(_uploaded_files))})

                # 6. 完成记录。如果需要，删除 record 文件夹
                if all_completed:
                    _log.info("==> All files uploaded")

                    if not self._upload_finish_flag_file(rec_cache.record["name"], rec_cache):
                        _log.error(f"==> Failed to upload finish flag file: {rec_cache.key}")
                        return

                    self.api.update_record(
                        record_name=rec_cache.record["name"],
                        labels=rec_cache.labels + ["上传完成"],
                    )
                    task_name = rec_cache.task.get("name", "")
                    if task_name:
                        self.api.put_task_tags(
                            task_name, {"recordName": rec_cache.record.get("name"), "totalFiles": str(rec_cache.total_files)}
                        )
                        self.api.update_task_state(task_name, "SUCCEEDED")
                    if rec_cache.diagnosis_task.get("name", ""):
                        self.api.update_task_state(rec_cache.diagnosis_task.get("name"), "SUCCEEDED")

                    rec_cache.uploaded = True
                    rec_cache.save_state()
                    _log.info(f"==> Handled record: {rec_cache.key}")

                    if self.conf.delete_after_upload:
                        rec_cache.delete_cache_dir(delay_in_hours=0)

    # noinspection PyBroadException
    def run(self, network_queue: Queue, error_queue: Queue):
        _log.info(f"==> Search for new record in {RECORD_DIR_PATH}")
        total_records = 0
        for record in RecordCache.find_all():
            try:
                self.handle_record(record)
                total_records += 1
            except Unauthorized:
                _log.error(f"==> Unauthorized when handling: {record.key}", exc_info=True)
                state = ApiClientState().load_state()
                state.authorized_device(0, "")
                state.save_state()
            except RecordNotFound:
                _log.error(f"==> Record already been deleted: {record.key}, skip handle!")
                record.delete_cache_dir(delay_in_hours=0)
            except Exception as e:
                # 打印错误，但保证循环不被打断
                _log.error(f"An error occurred when handling: {record.key}", exc_info=True)
                error_queue.put({"code": type(e).__name__, "error_msg": str(e)})

            # 不管上述结果如何，超过一定时间后，删除 record 文件夹
            _log.debug(f"==> Record previously uploaded: {record.key}")
            record.delete_cache_dir(self.conf.delete_after_interval_in_hours)

            try:
                network_queue.put(
                    {"upload": request_hook.get_network_upload_usage(), "download": request_hook.get_network_download_usage()}
                )
                request_hook.reset_network_usage()
            except Exception:
                _log.error("update network usage error", exc_info=True)
