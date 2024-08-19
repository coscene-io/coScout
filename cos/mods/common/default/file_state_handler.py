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
import os
import threading
from pathlib import Path
from typing import Callable

from cos.constant import FILE_STATE_PATH
from cos.core.api import ApiClient
from cos.mods.common.default.handlers import HANDLERS, HandlerInterface, Ros2Handler

_log = logging.getLogger(__name__)


class FileStateHandler:
    """
    FileStateHandler is used to keep track of the state of files in a directory.
    It keeps track of the start and end time of the files (in seconds since epoch),
    and can be used to query files that overlap with a given time range.
    Note that end time might be nil to indicate that the file is still active.
    Also note that currently we only track .log .mcap files, and we assume that
    only .mcap files might be static.
    Known limitations: does not work well with deleting files.
    E.g.:
    {
        "test.log": {
            "size" : 1234,
            "start_time": 1692089151,
            "end_time": 1692089210,
        },
        "test.mcap": {
            "processed": true,
            "size" : 1234,
            "start_time": 1692089151
            "end_time": 1692089210
        },
        "test.zip": {
            "size" : 1234,
            "unsupported": true
        },
    }
    """

    @property
    def state_path(self) -> Path:
        return FILE_STATE_PATH

    _instance = None
    _lock = threading.Lock()

    def __register_file_handlers(self, ros2_msg_dirs: list[str]):
        self.file_handlers: list[HandlerInterface] = HANDLERS
        Ros2Handler.register_ros2_types(ros2_msg_dirs)

    def __init__(self, ros2_msg_dirs: list[str]):
        if not FileStateHandler._instance:
            with FileStateHandler._lock:
                if not FileStateHandler._instance:
                    FileStateHandler._instance = self
                    self.state: dict[str, dict] = dict()
                    self.update_lock = threading.Lock()
                    self.__register_file_handlers(ros2_msg_dirs)
                    self.src_dirs = set()
                    self.load_state()

    @classmethod
    def get_instance(cls, ros2_msg_dirs=None):
        if ros2_msg_dirs is None:
            ros2_msg_dirs = []
        if not cls._instance:
            cls._instance = cls(ros2_msg_dirs)
        return cls._instance

    def load_state(self, state_path: Path = None):
        state_path = state_path or self.state_path
        if state_path.exists():
            with state_path.open("r") as fp:
                self.state = json.load(fp)

    def save_state(self, state_path: Path = None) -> None:
        state_path = state_path or self.state_path
        state_path.parent.mkdir(parents=True, exist_ok=True)
        with state_path.open("w") as fp:
            json.dump(self.state, fp, indent=2)

    def __set_file_state(self, file_path: Path, update_value: dict):
        filename_abs = str(file_path.absolute())
        with self.update_lock:
            self.state[filename_abs] = update_value

    def __del_file_state(self, file_path):
        filename_abs = str(file_path.absolute())
        with self.update_lock:
            del self.state[filename_abs]

    def get_file_state(self, file_path: Path):
        filename_abs = str(file_path.absolute())
        return self.state.get(filename_abs)

    def __update_file_state(self, file_path: Path, key: str, value):
        file_state = self.get_file_state(file_path)
        if not file_state:
            _log.warning(f"File {file_path.absolute()} not found in state, skip updating.")
            return
        self.__set_file_state(file_path, {**file_state, key: value})

    def __update_deleted_file_state(self):
        filenames = list(self.state.keys())
        for filename in filenames:
            file_path = Path(filename)
            if not file_path.exists():
                self.__del_file_state(file_path)

    def update_dirs(self, new_src_dirs_set: set[Path] = None):
        """Update the source directories to scan for files."""
        if new_src_dirs_set is None:
            new_src_dirs_set = self.src_dirs

        # Delete the state of files in the directories that are no longer scanned
        for src_dir in self.src_dirs - new_src_dirs_set:
            for filename in self.state.keys():
                if Path(filename).is_relative_to(src_dir):
                    self.__del_file_state(Path(filename))

        # Skip directories that user have no read access or do not exist
        new_src_dirs_set = {src_dir for src_dir in new_src_dirs_set if src_dir.exists() and os.access(src_dir, os.R_OK)}

        # Update the state of files in the new directories
        for src_dir in new_src_dirs_set:
            for entry in src_dir.iterdir():
                for handler in self.file_handlers:
                    # Skip if the file handler does not support entry
                    if not handler.check_file_path(entry):
                        continue

                    # Skip if file state is already up-to-date
                    file_state = self.get_file_state(entry)
                    if file_state and file_state.get("size") == handler.get_file_size(entry):
                        continue

                    try:
                        handler.update_path_state(entry, self.__set_file_state)
                    except Exception as e:
                        _log.error(f"Failed to update file state for {entry}, error: {e}")
                        self.__set_file_state(
                            entry,
                            {
                                "size": entry.stat().st_size,
                                "unsupported": True,
                            },
                        )

                # For files that are not supported by any handler, mark them as unsupported
                if not self.get_file_state(entry):
                    self.__set_file_state(
                        entry,
                        {
                            "size": entry.stat().st_size,
                            "unsupported": True,
                        },
                    )

        # Mark all files in the newly scanned directories as processed
        for src_dir in new_src_dirs_set - self.src_dirs:
            for filename in self.state.keys():
                if Path(filename).is_relative_to(src_dir):
                    self.__update_file_state(Path(filename), "processed", True)

        self.src_dirs = new_src_dirs_set
        self.__update_deleted_file_state()
        self.save_state()

    def static_file_diagnosis(self, api_client: ApiClient, file_path: Path, upload_fn):
        file_state = self.get_file_state(file_path)
        for handler in self.file_handlers:
            # Skip if the file handler is not static or does not support the file
            if not handler.supports_static() or not handler.check_file_path(file_path):
                continue

            # Skip if file is already processed and size unchanged
            if file_state.get("processed") and handler.get_file_size(file_path) == file_state.get("size"):
                return

            _log.info(f"Found unprocessed file {file_path}, processing with {handler}")
            self.__update_file_state(file_path, "processed", True)
            self.save_state()
            handler.diagnose(api_client, file_path, upload_fn)
            _log.info(f"Finished processing file {file_path}")

    def get_files(self, *filters: Callable[[dict], bool]):
        """
        Get a list of supported files that match the given filters.
        :param filters: A list of filters to apply to the file state.
        :return: A list of supported files that match the given filters.
        """
        return [
            filename
            for filename, file_state in self.state.items()
            if not file_state.get("unsupported") and all(filter_func(file_state) for filter_func in filters)
        ]

    @staticmethod
    def state_unprocessed_filter() -> Callable[[dict], bool]:
        """
        Create a filter that checks if the file state is unprocessed.
        :return: A filter that checks if the file state is unprocessed.
        """
        return lambda file_state: not file_state.get("processed")

    @staticmethod
    def state_timestamp_filter(start_time: int, end_time: int) -> Callable[[dict], bool]:
        """
        Create a filter that checks if the file state overlaps with the given time range.
        :param start_time: The start time of the time range in seconds.
        :param end_time: The end time of the time range in seconds.
        :return: A filter that checks if the file state overlaps with the given time range.
        """
        return lambda file_state: file_state.get("start_time") <= end_time and file_state.get("end_time") >= start_time

    @staticmethod
    def state_dir_filter(get_dir: bool) -> Callable[[dict], bool]:
        """
        Create a filter that checks if the file state is a directory.
        :param get_dir: Whether to fetch directories.
        :return: A filter that checks if the file state is a directory.
        """
        return lambda file_state: bool(file_state.get("is_dir")) == get_dir
