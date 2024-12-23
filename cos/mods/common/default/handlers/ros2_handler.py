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
from rosbags.rosbag2 import Reader as Ros2Reader
from rosbags.serde import deserialize_cdr
from rosbags.typesys import get_types_from_msg, register_types

from cos.mods.common.default.handlers.handler_interface import HandlerInterface
from cos.mods.common.default.rule_executor import RuleDataItem
from cos.utils.files import can_read_path

_log = logging.getLogger(__name__)


class Ros2Handler(BaseModel, HandlerInterface):
    def __str__(self):
        return "ROS2 Handler"

    @staticmethod
    def __compute_ros2_dir_size(file_path: Path) -> int:
        size = 0
        for entry in file_path.iterdir():
            if not entry.is_file():
                continue
            if entry.name.endswith(".db3") or entry.name == "metadata.yaml":
                size += entry.stat().st_size
        return size

    @staticmethod
    def check_file_path(file_path: Path) -> bool:
        # First check if the path is accessible
        if not can_read_path(str(file_path)):
            return False
        if not file_path.is_dir():
            return False
        contains_db3 = False
        contains_metadata = False
        for entry in file_path.iterdir():
            if not entry.is_file():
                continue
            if entry.name == "metadata.yaml":
                contains_metadata = True
            elif entry.name.endswith(".db3"):
                contains_db3 = True
        return contains_db3 and contains_metadata

    def compute_path_state(self, file_path: Path):
        dir_size = self.__compute_ros2_dir_size(file_path)
        with Ros2Reader(file_path) as reader:
            start_time = reader.start_time // 1_000_000_000
            end_time = reader.end_time // 1_000_000_000
            return {
                "size": dir_size,
                "start_time": start_time,
                "end_time": end_time,
                "is_dir": True,
            }

    def get_file_size(self, file_path: Path) -> int:
        return self.__compute_ros2_dir_size(file_path)

    def msg_iterator(self, file_path: Path, active_topics: set[str]):
        skipped_topics = set()
        with Ros2Reader(file_path) as reader:
            connections = [conn for conn in reader.connections() if conn.topic in active_topics]
            for connection, timestamp, rawdata in reader.messages(connections=connections):
                try:
                    msg = deserialize_cdr(rawdata, connection.msgtype)
                    yield RuleDataItem(
                        topic=connection.topic, msg=msg, ts=timestamp // 1_000_000_000, msgtype=connection.msgtype
                    )
                except Exception:
                    if connection.topic not in skipped_topics:
                        skipped_topics.add(connection.topic)
                        _log.warning(f"==> Failed to deserialize message for {connection.topic}, skipping")

    @staticmethod
    def register_ros2_types(ros2_msg_dirs: list[str]):
        """Register ROS2 message types"""
        customized_types = {}
        for ros2_msg_dir_name in ros2_msg_dirs:
            ros2_msg_dir = Path(ros2_msg_dir_name)
            for msg_path in ros2_msg_dir.glob("**/*.msg"):
                msg_def = msg_path.read_text(encoding="utf-8")
                msg_type_name = (Path(ros2_msg_dir.name) / "msg" / msg_path.name).with_suffix("")
                customized_types.update(get_types_from_msg(msg_def, str(msg_type_name)))
        _log.debug("Registering ROS2 types: \n" + "\n".join(customized_types.keys()))
        register_types(customized_types)
