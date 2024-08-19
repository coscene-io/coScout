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
import shutil
import time
from functools import partial
from pathlib import Path
from typing import Callable

import pygtail
from pydantic import BaseModel

from cos.core.api import ApiClient
from cos.mods.common.default.handlers.handler_interface import HandlerInterface
from cos.mods.common.default.log_utils import (
    get_end_timestamp,
    get_file_encoding,
    get_start_timestamp,
    get_timestamp_from_line,
    get_timestamp_hint_from_file,
)
from cos.mods.common.default.rule_executor import LogMessageDataItem, RuleDataItem, RuleExecutor

_log = logging.getLogger(__name__)


class LogHandler(BaseModel, HandlerInterface):
    def __str__(self):
        return "LOG Handler"

    @staticmethod
    def supports_static() -> bool:
        return False

    @staticmethod
    def check_file_path(file_path: Path) -> bool:
        return file_path.is_file() and file_path.name.endswith(".log")

    def update_path_state(self, file_path: Path, update_func: Callable[[Path, dict], None]):
        start_time = get_start_timestamp(file_path)
        if start_time is None:
            raise Exception(f"Failed to get start timestamp for log file: {file_path}.")

        end_time = get_end_timestamp(file_path)
        if end_time is None:
            raise Exception(f"Failed to get end timestamp for log file: {file_path}.")

        update_func(
            file_path,
            {
                "size": file_path.stat().st_size,
                "start_time": start_time,
                "end_time": end_time,
            },
        )

    def msg_iterator(self, file_path: Path):
        _log.warning("==> This method is not supported for log handler")
        return

    @staticmethod
    def get_files_to_scan() -> set[Path]:
        from cos.mods.common.default.file_state_handler import FileStateHandler

        file_state_handler = FileStateHandler.get_instance()
        all_files = file_state_handler.get_files(FileStateHandler.state_unprocessed_filter())
        return {Path(filename) for filename in all_files if LogHandler.check_file_path(Path(filename))}

    def get_log_input_stream(self):
        tail_dict = dict()
        latest_timestamps = dict()

        while True:
            # Update tail_dict with new files
            new_tail_dict = dict()
            for log_file in self.get_files_to_scan():
                log_file_abs = str(log_file.absolute())
                if log_file_abs in tail_dict:
                    new_tail_dict[log_file_abs] = tail_dict[log_file_abs]
                else:
                    _log.info(f"==> Found new log file: {log_file}")
                    new_tail_dict[log_file_abs] = self.__get_tail_and_hint(log_file)
            tail_dict = new_tail_dict
            time.sleep(5)

            for filename, (tail, hint) in tail_dict.items():
                try:
                    for line in tail:
                        timestamp = get_timestamp_from_line(line, hint)
                        if timestamp:
                            latest_timestamps[filename] = int(timestamp.timestamp())
                        if filename not in latest_timestamps:
                            continue

                        yield RuleDataItem(
                            filename,
                            LogMessageDataItem(line),
                            latest_timestamps[filename],
                            "foxglove.Log",
                        )
                except FileNotFoundError:
                    _log.warning(f"==> File not found, might be deleted: {filename}")
                    del tail_dict[filename]
                    if filename in latest_timestamps:
                        del latest_timestamps[filename]
                    break

    def scan_dirs_and_diagnose(self, api_client: ApiClient, upload_fn: partial):
        executor_name = "Log Scan Rule Executor"
        rule_executor = RuleExecutor(executor_name, api_client, self.get_log_input_stream(), upload_fn)
        rule_executor.execute()

    @staticmethod
    def __get_tail_and_hint(file_path: Path):
        """Get the Pygtail object and timestamp hint for the given file path,
        note that the file path should be checked before calling this method."""
        filename_abs = str(file_path.absolute())
        file_encoding = get_file_encoding(file_path)
        return (
            pygtail.Pygtail(
                filename_abs,
                save_on_end=False,
                encoding=file_encoding,
            ),
            get_timestamp_hint_from_file(file_path, file_encoding),
        )

    @staticmethod
    def prepare_cut(src_file_path: Path, target_dir: Path, start_time: int, end_time: int) -> str:
        """TODO Slice log file and return the path of the sliced file"""
        transcode_chunk_size = 1024 * 1024
        dst_file_path = target_dir / src_file_path.name
        file_encoding = get_file_encoding(src_file_path)
        if file_encoding == "utf8":
            shutil.copy(src_file_path, dst_file_path)
        else:
            with open(src_file_path, "r", encoding=file_encoding) as src_f:
                with open(dst_file_path, "w", encoding="utf8") as dst_f:
                    chunk = src_f.read(transcode_chunk_size)
                    while chunk:
                        dst_f.write(chunk)
                        chunk = src_f.read(transcode_chunk_size)
        return str(dst_file_path.absolute())
