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
from pathlib import Path

from pydantic import BaseModel

from cos.mods.common.default.handlers.handler_interface import HandlerInterface
from cos.mods.common.default.log_utils import LogReader
from cos.mods.common.default.rule_executor import RuleDataItem

_log = logging.getLogger(__name__)


class LogHandler(BaseModel, HandlerInterface):
    def __str__(self):
        return "LOG Handler"

    @staticmethod
    def check_file_path(file_path: Path) -> bool:
        return file_path.is_file() and file_path.name.endswith(".log")

    def compute_path_state(self, file_path: Path):
        reader = LogReader(file_path)
        start_time = reader.get_start_timestamp()
        if start_time is None:
            raise Exception(f"Failed to get start timestamp for log file: {file_path}.")

        end_time = reader.get_end_timestamp()
        if end_time is None:
            raise Exception(f"Failed to get end timestamp for log file: {file_path}.")

        return {
            "size": file_path.stat().st_size,
            "start_time": start_time,
            "end_time": end_time,
        }

    def msg_iterator(self, file_path: Path, active_topics: set[str]):
        reader = LogReader(file_path)
        if active_topics and "/external_log" not in active_topics:
            return
        for stamped_log in reader.stamped_log_generator():
            ts = stamped_log.timestamp.timestamp()
            yield RuleDataItem(
                topic="/external_log",
                msg={
                    "timestamp": {
                        "sec": int(ts),
                        "nsec": int((ts - int(ts)) * 1e9),
                    },
                    "message": stamped_log.line,
                    "file": file_path.name,
                    "level": LogHandler._get_log_level(stamped_log.line),
                },
                ts=stamped_log.timestamp.timestamp(),
                msgtype="foxglove.Log",
            )

    @staticmethod
    def _get_log_level(line: str) -> int:
        """
        Get the log level from the log line.
        Current implementation is simple.
        """
        if "DEBUG" in line:
            return 1
        if "WARN" in line:
            return 3
        if "ERROR" in line:
            return 4
        if "FATAL" in line:
            return 5
        return 2

    # @staticmethod
    # def prepare_cut(src_file_path: Path, target_dir: Path, start_time: int, end_time: int) -> str:
    #     """TODO Slice log file and return the path of the sliced file"""
    #     transcode_chunk_size = 1024 * 1024
    #     dst_file_path = target_dir / src_file_path.name
    #     file_encoding = get_file_encoding(src_file_path)
    #     if file_encoding == "utf8":
    #         shutil.copy(src_file_path, dst_file_path)
    #     else:
    #         with open(src_file_path, "r", encoding=file_encoding) as src_f:
    #             with open(dst_file_path, "w", encoding="utf8") as dst_f:
    #                 chunk = src_f.read(transcode_chunk_size)
    #                 while chunk:
    #                     dst_f.write(chunk)
    #                     chunk = src_f.read(transcode_chunk_size)
    #     return str(dst_file_path.absolute())
