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
import time
from datetime import datetime
from pathlib import Path
from typing import List

from cos.core.api import ApiClient
from cos.core.models import FileInfo, RecordCache
from cos.utils.files import can_read_path

_log = logging.getLogger(__name__)


class TaskHandler:
    def __init__(self, api_client: ApiClient, upload_files: list[str]):
        self._api = api_client
        self._upload_files = upload_files

    def run(self):
        _log.info("==> Start check upload tasks.")
        api_state = self._api.state.load_state()
        device = api_state.device
        if not device or not device.get("name"):
            _log.warning("device name not found, skipping")
            return

        tasks = self._api.list_device_tasks(device.get("name"), "PENDING")
        for task in tasks:
            self._handle_upload_task(task)
        _log.info("==> Task mod check upload tasks done.")

        cancelling_tasks = self._api.list_device_tasks(device.get("name"), "CANCELLING")
        self._handle_cancel_tasks(cancelling_tasks)
        _log.info("==> Task mod check cancelling tasks done.")

    def _handle_cancel_tasks(self, tasks: List[dict]):
        task_names = [task.get("name", "") for task in tasks]
        for rc in RecordCache.find_all():
            try:
                task_name = rc.task.get("name", "")
                if not task_name:
                    continue
                if task_name not in task_names:
                    continue

                rc.skipped = True
                rc.save_state()

                self._api.update_task_state(task_name, "CANCELLED")
            except Exception:
                _log.error("handle cancel tasks error", exc_info=True)

    def _handle_upload_task(self, task):
        start_time = self._parse_timestr(task.get("uploadTaskDetail", {}).get("startTime", ""))
        end_time = self._parse_timestr(task.get("uploadTaskDetail", {}).get("endTime", ""))

        task_name = task.get("name")
        if not task_name:
            _log.warning("Task name not found, skipping")
            return

        task_folders = task.get("uploadTaskDetail", {}).get("scanFolders", [])
        scan_folders = list(set(task_folders + self._upload_files))
        self._api.update_task_state(task_name, "PROCESSING")
        files = []
        for file in scan_folders:
            file_path = Path(file)
            can_read = can_read_path(str(file_path.absolute()))
            if not can_read:
                _log.warning(f"File {file_path} is not existed or readable, skipping")
                continue

            if file_path.is_dir():
                files += self._resolve_dir(file_path, start_time, end_time)
            elif file_path.is_file():
                files.append(FileInfo(filepath=str(file_path.resolve().absolute()), filename=file_path.name))
            else:
                _log.warning(f"File {file_path} is not a file or directory, skipping")

        if len(files) == 0:
            _log.info("==> No files found, skipping")
            self._api.update_task_state(task_name, "SUCCEEDED")
            return

        files = self._unqiue_files(files)
        # task_name: warehouses/xxx/projects/xxx/tasks/xxx, project_name: warehouses/xxx/projects/xxx
        project_name = task_name.split("/tasks/")[0]
        rc = RecordCache(
            project_name=project_name,
            timestamp=int(time.time() * 1000),
            labels=[],
            task={
                "name": task_name,
                "title": task.get("title", ""),
            },
        ).load_state()
        rc.file_infos = files
        rc.total_files = len(files)
        rc.save_state()

        self._api.put_task_tags(task_name, {"totalFiles": str(len(files))})
        _log.info(f"==> Converted error log to record state: {rc.state_path}")

    def _parse_timestr(self, time_str: str) -> float:
        if not time_str:
            return 0.0

        if time_str.endswith("Z"):
            time_str = time_str.replace("Z", "+00:00")  # Replace 'Z' with '+00:00' to indicate UTC

        # Parse the time string to a datetime object
        dt = datetime.fromisoformat(time_str)
        return dt.timestamp()

    def _resolve_dir(self, dir_path: Path, start_time: float, end_time: float) -> list[FileInfo]:
        files = []
        for file in dir_path.rglob("*"):
            if file.is_file():
                mtime = file.stat().st_mtime
                if start_time <= mtime <= end_time:
                    _filename = str(file.relative_to(dir_path))
                    files.append(FileInfo(filepath=str(file.resolve().absolute()), filename=_filename))
                else:
                    _log.info(f"==> Skip file {file}, mtime: {mtime}, start_time: {start_time}, end_time: {end_time}")
        return files

    def _unqiue_files(self, files: list[FileInfo]) -> list[FileInfo]:
        seen = {}
        for obj in files:
            if obj.filename not in seen:
                seen[obj.filename] = obj
        return list(seen.values())
