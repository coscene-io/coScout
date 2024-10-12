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

from pydantic import BaseModel

from cos.constant import RECORD_DIR_PATH
from cos.core.api import ApiClient, ApiClientState
from cos.core.models import FileInfo, RecordCache
from cos.utils import hardlink, is_image
from .codes import EventCodeManager
from ..core import request_hook
from ..core.exceptions import Unauthorized
from ..utils.files import can_read_path

_log = logging.getLogger(__name__)


class CollectorConfig(BaseModel):
    delete_after_upload: bool = True
    delete_after_interval_in_hours: int = -1
    scan_interval_in_secs: int = 60


class Collector:
    def __init__(
        self,
        conf: CollectorConfig,
        api_client: ApiClient,
        code_manager: EventCodeManager,
    ):
        self.conf = conf
        self.api = api_client
        self.code_mgr = code_manager
        self.device = self.api.state.load_state().device

    def _upload_record_thumbnail(self, record_name, rec_cache: RecordCache):
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
            remove_after=True,
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

    def _create_record_and_event(self, rec_cache: RecordCache):
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
        self._upload_record_thumbnail(record.get("name"), rec_cache)

        for moment in rec_cache.moments:
            _ = self.api.create_event(
                record_name=record.get("name"),
                display_name=moment.title if moment.title else record_title,
                description=moment.description if moment.description else record_title,
                customized_fields=moment.metadata,
                trigger_time=moment.timestamp / 1000,
                duration=moment.duration / 1000,
                device_name=self.device["name"],
            )

            if moment.task:
                created_task = self.api.create_task(
                    record_name=record.get("name"),
                    title=moment.title if moment.title else record_title,
                    description=moment.description if moment.description else record_title,
                    assignee=moment.task.assignee,
                )
                if moment.task.sync_task and created_task.get("name"):
                    try:
                        _log.info(f"==> Sync task: {created_task.get('name')}")
                        self.api.sync_task(created_task.get("name"))
                        _log.info(f"==> Sync task done: {created_task.get('name')}")
                    except Exception:
                        _log.error(f"Failed to sync task: {created_task.get('name')}", exc_info=True)

        if rec_cache.diagnosis_task:
            created_diagnosis_task = self.api.create_diagnosis_task(
                title=record_title,
                description=self.__make_record_description(record_title, rec_cache),
                device=self.device.get("name"),
                rule_id=rec_cache.diagnosis_task.get("rule_id"),
                rule_name=rec_cache.diagnosis_task.get("rule_name"),
                trigger_time=rec_cache.diagnosis_task.get("trigger_time"),
                start_time=rec_cache.diagnosis_task.get("start_time"),
                end_time=rec_cache.diagnosis_task.get("end_time"),
            )
            rec_cache.diagnosis_task["name"] = created_diagnosis_task.get("name")
        return record

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
                # 2. 收集文件，生成 hardlink, 替换FileInfo里filepath为 hardlink 的目标文件（RecordCache.files依然指向原始文件）
                rec_cache.file_infos = [
                    FileInfo(
                        filepath=hardlink(f.filepath.resolve().absolute(), rec_cache.base_dir_path / f.filename),
                        filename=f.filename,
                    ).complete(inplace=True, skip_sha256=True)
                    for f in rec_cache.file_infos
                    if f.filepath.is_file() and f.filename != "finish.flag" and can_read_path(str(f.filepath.absolute()))
                ]

                # 3. 创建 record 和 event
                rec_cache.record = self._create_record_and_event(rec_cache)
                # 为防止断连后重新建立记录，需要立即写回json
                rec_cache.save_state()
                self.code_mgr.hit(rec_cache.event_code)

                task_name = rec_cache.task.get("name", "")
                if task_name:
                    self.api.put_task_tags(task_name, {"recordName": rec_cache.record.get("name", "")})

            # 找齐文件后，如果还没有 uploaded (上传完毕).
            if not rec_cache.uploaded:
                # 5. 上传文件传输完毕后更新
                filepaths = rec_cache.list_files()
                rec_cache.file_infos = [
                    f for f in rec_cache.file_infos if str(f.filepath) and can_read_path(str(f.filepath)) in filepaths
                ]

                file_infos = [f.complete(inplace=True, skip_sha256=True) for f in rec_cache.file_infos]
                sorted_files: List[FileInfo] = sorted(file_infos, key=lambda f: f.size)

                all_completed = True
                for file_info in sorted_files:
                    if not rec_cache.state_path.absolute().exists():
                        return

                    _rc = RecordCache.load_state_from_disk(rec_cache.state_path.absolute())
                    if _rc.skipped:
                        return

                    _completed = self.api.resumable_upload_files(
                        record_name=rec_cache.record["name"],
                        file_infos=[file_info],
                        remove_after=True,
                    )
                    all_completed = all_completed and _completed

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
                        self.api.put_task_tags(task_name, {"recordName": rec_cache.record.get("name")})
                        self.api.update_task_state(task_name, "SUCCEEDED")
                    if rec_cache.diagnosis_task.get("name", ""):
                        self.api.update_task_state(rec_cache.diagnosis_task.get("name"), "SUCCEEDED")

                    rec_cache.uploaded = True
                    rec_cache.save_state()
                    _log.info(f"==> Handled record: {rec_cache.key}")

                    if self.conf.delete_after_upload:
                        rec_cache.delete_cache_dir()

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
