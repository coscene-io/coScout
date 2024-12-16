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
import threading
import time
from pathlib import Path
from typing import Callable

from cos.constant import FILE_STATE_PATH
from cos.core.api import ApiClient
from cos.mods.common.default.handlers import HANDLERS, HandlerInterface, LogHandler, Ros2Handler
from cos.utils.files import can_read_path

_log = logging.getLogger(__name__)


class FileStateHandler:
    """
    FileStateHandler is used to keep track of the state of files in a directory.
    It keeps track of the start and end time of the files (in seconds since epoch),
    and can be used to query files that overlap with a given time range.
    Note that end time might be nil to indicate that the file is still active.
    Also note that currently we only track .log .mcap files, and we assume that
    only .mcap files might be static.
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

    Key definitions:
    - size: The size of the file in bytes.
    - start_time: The start time of the file in seconds since epoch.
    - end_time: The end time of the file in seconds since epoch.
    - unsupported: Whether the file is unsupported by the diagnosis engine.
    - is_dir: Whether the file entry is a directory.
    - is_listening: Whether the file entry is being listened to.
    - processed: Whether the file has been processed by the diagnosis engine.
                 Note that this is only used if is_listening is True.
    - is_collecting: Whether the file entry is being collected.
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
                    self.listen_dirs = set()
                    self.active_topics = set()  # topics that are currently being listened to
                    self.load_state()
                    self.save_state()

    def get_file_handler(self, file_path: Path):
        for handler in self.file_handlers:
            if handler.check_file_path(file_path):
                return handler
        return None

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
                loaded_state = json.load(fp)
                if "src_dirs" in loaded_state:
                    self.src_dirs = {Path(src_dir) for src_dir in loaded_state.get("src_dirs", [])}
                    self.listen_dirs = {Path(listen_dir) for listen_dir in loaded_state.get("listen_dirs", [])}
                    self.state = loaded_state.get("state", {})
                else:
                    self.state = loaded_state

    def save_state(self, state_path: Path = None) -> None:
        state_path = state_path or self.state_path
        state_path.parent.mkdir(parents=True, exist_ok=True)
        state_to_save = {
            "state": self.state,
            "src_dirs": [str(src_dir) for src_dir in self.src_dirs],
            "listen_dirs": [str(listen_dir) for listen_dir in self.listen_dirs],
        }
        with state_path.open("w") as fp:
            json.dump(state_to_save, fp, indent=2)

    def __set_file_state(self, file_path: Path, update_value: dict):
        filename_abs = str(file_path.absolute())
        with self.update_lock:
            self.state[filename_abs] = update_value

    def __del_file_state(self, file_path):
        filename_abs = str(file_path.absolute())
        with self.update_lock:
            del self.state[filename_abs]

    def __get_file_state(self, file_path: Path):
        filename_abs = str(file_path.absolute())
        return self.state.get(filename_abs)

    def __update_file_state(self, file_path: Path, key: str, value):
        file_state = self.__get_file_state(file_path)
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

    def update_dirs(self, listen_dirs: set[Path], collect_dirs: set[Path]):
        _log.info(f"Start updating directories, listen_dirs: {listen_dirs}, collect_dirs: {collect_dirs}")

        # Skip directories that user have no read access or do not exist
        listen_dirs = {listen_dir for listen_dir in listen_dirs if can_read_path(str(listen_dir))}
        collect_dirs = {collect_dir for collect_dir in collect_dirs if can_read_path(str(collect_dir))}

        # Delete the state of files in the directories that are no longer scanned or collected
        for src_dir in self.src_dirs - listen_dirs - collect_dirs:
            for filename in list(self.state.keys()):
                if Path(filename).parent == src_dir:
                    self.__del_file_state(Path(filename))

        # Iterate over the new directories and update the state of files
        for src_dir in listen_dirs | collect_dirs:
            if not src_dir.is_dir():
                _log.warning(f"{str(src_dir.absolute())} is not dir, skip handle.")
                continue

            for entry in src_dir.iterdir():
                file_state = self.__get_file_state(entry)

                # Skip unsupported files
                if file_state and file_state.get("unsupported"):
                    continue

                handler = self.get_file_handler(entry)
                if not handler:
                    # File is not supported by any handler, mark it as unsupported if not already and skip
                    if not file_state or not file_state.get("unsupported"):
                        self.__set_file_state(
                            entry,
                            {
                                "size": entry.stat().st_size,
                                "unsupported": True,
                            },
                        )
                    continue

                # Check if file is last modified within 2 hours, if not, mark it as unsupported and skip
                if entry.is_file() and entry.stat().st_mtime < time.time() - 2 * 3600:
                    self.__set_file_state(
                        entry,
                        {
                            "size": entry.stat().st_size,
                            "unsupported": True,
                        },
                    )
                    continue

                is_listening = src_dir in listen_dirs
                is_collecting = src_dir in collect_dirs
                # need process when file is in the listen dirs all the time and
                # is either newly added or changed or the file is not processed yet
                need_process = src_dir in listen_dirs & self.listen_dirs and (
                    not file_state or file_state.get("size") != handler.get_file_size(entry) or not file_state.get("processed")
                )
                try:
                    if file_state and handler.get_file_size(entry) == file_state.get("size"):
                        state_to_set = file_state
                    else:
                        state_to_set = handler.compute_path_state(entry)
                    self.__set_file_state(
                        entry,
                        {
                            **state_to_set,
                            "is_listening": is_listening,
                            "is_collecting": is_collecting,
                            "processed": not need_process,
                        },
                    )
                except Exception as e:
                    _log.error(f"Failed to update file state for {entry}, error: {e}")
                    self.__set_file_state(
                        entry,
                        {
                            "size": entry.stat().st_size,
                            "unsupported": True,
                        },
                    )
        self.src_dirs = listen_dirs | collect_dirs
        self.listen_dirs = listen_dirs
        self.__update_deleted_file_state()
        self.save_state()
        _log.info(f"Finished updating directories")

    def diagnose(self, api_client: ApiClient, file_path: Path, upload_fn, active_topics: set[str]):
        file_state = self.__get_file_state(file_path)
        handler = self.get_file_handler(file_path)
        if not handler:
            return

        if file_state.get("processed") and handler.get_file_size(file_path) == file_state.get("size"):
            return

        # If the file is unprocessed and is not ready to process, mark it as ready to process and return
        # This is to avoid processing the same changing file multiple times, only process
        # the changing file once it is stable.
        if not file_state.get("ready_to_process", False):
            self.__update_file_state(file_path, "ready_to_process", True)
            return

        _log.info(f"Found unprocessed file {file_path}, processing with {handler}")
        self.__update_file_state(file_path, "processed", True)
        self.__update_file_state(file_path, "ready_to_process", False)
        self.save_state()
        handler.diagnose(api_client, file_path, upload_fn, active_topics)
        _log.info(f"Finished processing file {file_path}")

    def get_files(self, *filters: Callable[[str, dict], bool]):
        """
        Get a list of supported files that match the given filters.
        :param filters: A list of filters to apply to the file state.
        :return: A list of supported files that match the given filters.
        """
        return [
            filename
            for filename, file_state in self.state.items()
            if not file_state.get("unsupported") and all(filter_func(filename, file_state) for filter_func in filters)
        ]

    @staticmethod
    def state_unprocessed_filter() -> Callable[[str, dict], bool]:
        """
        Create a filter that checks if the file state is unprocessed.
        :return: A filter that checks if the file state is unprocessed.
        """
        return lambda _, file_state: not file_state.get("processed")

    @staticmethod
    def state_timestamp_filter(start_time: int, end_time: int) -> Callable[[str, dict], bool]:
        """
        Create a filter that checks if the file state overlaps with the given time range.
        :param start_time: The start time of the time range in seconds.
        :param end_time: The end time of the time range in seconds.
        :return: A filter that checks if the file state overlaps with the given time range.
        """
        return lambda _, file_state: file_state.get("start_time") <= end_time and file_state.get("end_time") >= start_time

    @staticmethod
    def state_dir_filter(get_dir: bool) -> Callable[[str, dict], bool]:
        """
        Create a filter that checks if the file state is a directory.
        :param get_dir: Whether to fetch directories.
        :return: A filter that checks if the file state is a directory.
        """
        return lambda _, file_state: bool(file_state.get("is_dir")) == get_dir

    @staticmethod
    def state_is_listening_filter() -> Callable[[str, dict], bool]:
        """
        Create a filter that checks if the file state is being listened to.
        :return: A filter that checks if the file state is being listened to.
        """
        return lambda _, file_state: bool(file_state.get("is_listening"))

    @staticmethod
    def state_is_collecting_filter() -> Callable[[str, dict], bool]:
        """
        Create a filter that checks if the file state is being collected.
        :return: A filter that checks if the file state is being collected.
        """
        return lambda _, file_state: bool(file_state.get("is_collecting"))
