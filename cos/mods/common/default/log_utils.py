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
import re
from collections import deque
from datetime import datetime, timedelta, timezone
from pathlib import Path

import chardet
from sortedcontainers import SortedList

_log = logging.getLogger(__name__)

# Define the timestamp hint format, which is used to extract the timestamp hint
# from the filename or the first line of the file.
HINT_OPTIONS = [
    (re.compile(r"\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}"), "%Y-%m-%d %H:%M:%S"),
    (re.compile(r"\d{4}/\d{2}/\d{2}\s+\d{2}:\d{2}:\d{2}"), "%Y/%m/%d %H:%M:%S"),
    (re.compile(r"\d{10}"), "%Y%m%d%H"),
]

# Define the timestamp schema, which is used to extract the timestamp from a line.
TS_SCHEMA: list = [
    (
        re.compile(r"\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}\.\d{3}"),
        "%Y-%m-%d %H:%M:%S.%f",
    ),
    (
        re.compile(r"\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2},\d{3}"),
        "%Y-%m-%d %H:%M:%S,%f",
    ),
    (
        re.compile(r"\d{4}/\d{2}/\d{2}\s+\d{2}:\d{2}:\d{2}"),
        "%Y/%m/%d %H:%M:%S",
    ),
    (re.compile(r"\d{4}\s+\d{2}:\d{2}:\d{2}\.\d{6}"), "%m%d %H:%M:%S.%f"),
    (re.compile(r"[a-zA-Z]{3}\s+\d{1,2}\s+\d{2}:\d{2}:\d{2}"), "%b %d %H:%M:%S"),
    (re.compile(r"\d{2}:\d{2}:\d{2}\.\d{3}"), "%H:%M:%S.%f"),
]

# Define the accepted special encodings, which are used to detect the file encoding.
# Unaccepted special encodings will be treated as utf8.
ACCEPTED_SPECIAL_ENCODINGS = ["GB2312"]

# Define the chunk size used to predict the timestamp schema, the start timestamp, and the end timestamp.
CHUNK_SIZE = 32 * 1024  # 32KB


class StampedLog:
    def __init__(self, timestamp: datetime | None, line: str):
        self.timestamp = timestamp
        self.line = line


class OrderedQueue:
    """A queue that maintains the order of the StampedLog elements."""

    def __init__(self, size: int, k: int = 1):
        """
        Initialize the queue and a sorted list for tracking the order of the elements.
        """
        self.size = size + 1
        self.k: int = k
        self.queue: deque[StampedLog] = deque(maxlen=self.size)
        self.sorted_list: SortedList[StampedLog] = SortedList(key=lambda x: x.timestamp)

    def consume(self, log: StampedLog) -> StampedLog | None:
        """
        Consume a log into the OrderedQueue and pop the oldest log if the queue is full.
        Merge the log into the last StampedLog if log.timestamp is None. In this way,
        all logs in the queue are stamped.

        The return value can either be None or a StampedLog. And the algorithm
        guarantees that returned StampedLogs (timestamp not None) are in order.
        """
        if log.timestamp is None:
            # log without timestamp should be merged into the last log if present
            if self.queue:
                last_log = self.queue.pop()
                last_log.line += log.line
                self.queue.append(last_log)
            return None

        # log with timestamp should be added to the queue and the sorted list
        self.queue.append(log)
        self.sorted_list.add(log)

        # Check if the queue is full
        if len(self.queue) == self.size:
            # Pop the oldest log from the queue and the sorted list
            sl = self.queue.popleft()
            idx = self.sorted_list.index(sl)
            self.sorted_list.remove(sl)

            # Check if the log is within the k-th order
            if idx <= self.k:
                # Ready to return the log, merge all logs at the beginning
                # that have smaller timestamp than the current log
                # This ensures that the logs are returned in order
                while self.queue and self.queue[0].timestamp < sl.timestamp:
                    small_sl = self.queue.popleft()
                    sl.line += small_sl.line
                    self.sorted_list.remove(small_sl)

                return sl
            else:
                return StampedLog(None, sl.line)

    def dump_remaining(self):
        """
        Dump all the remaining logs in the queue. Need to ensure logs are dumped in order.
        """
        remaining_logs = []
        while self.queue:
            sl = self.queue.popleft()
            idx = self.sorted_list.index(sl)
            self.sorted_list.remove(sl)

            if idx <= self.k:
                while self.queue and self.queue[0].timestamp < sl.timestamp:
                    small_sl = self.queue.popleft()
                    sl.line += small_sl.line
                    self.sorted_list.remove(small_sl)
                remaining_logs.append(sl)
            else:
                remaining_logs.append(StampedLog(None, sl.line))

        return remaining_logs


class LogReader:
    """LogReader is a class that reads log files and returns log messages."""

    def __init__(self, file_path: Path):
        self.file_path = file_path
        self.file_size = file_path.stat().st_size
        self.encoding = self.get_file_encoding()
        self.hint = self.get_timestamp_hint_from_file()
        self.buffer_size = 20
        self.accepted_order = 1
        self.regex, self.date_format = self.analyze_ts_schema()

    def get_file_encoding(self) -> str:
        """Get the file encoding of the log file."""
        with self.file_path.open("rb") as f:
            file_encoding = chardet.detect(f.read(CHUNK_SIZE))["encoding"]
            if file_encoding in ACCEPTED_SPECIAL_ENCODINGS:
                return file_encoding
            return "utf8"

    @staticmethod
    def get_hint_from_text(text: str) -> datetime | None:
        """Get the timestamp hint from the text."""
        for regex, date_format in HINT_OPTIONS:
            time_candidate = regex.search(text)
            if time_candidate:
                try:
                    return datetime.strptime(time_candidate.group(), date_format).replace(tzinfo=timezone(timedelta(hours=8)))
                except ValueError:
                    pass
        return None

    def get_timestamp_hint_from_file(self) -> datetime | None:
        """
        Get the timestamp hint from filename or the first line of the file.
        Priority: filename > first line > None
        """
        filename_hint = LogReader.get_hint_from_text(self.file_path.name)
        if filename_hint:
            return filename_hint

        with self.file_path.open("r", encoding=self.encoding) as f:
            first_line = f.readline()

        return LogReader.get_hint_from_text(first_line)

    @staticmethod
    def get_timestamp_with_hint(dt: datetime, hint: datetime, date_format: str):
        if hint:
            # We have the assumption that either the entire date info is missing
            # or only the year is missing
            if "%d" not in date_format:
                candidate = dt.replace(year=hint.year, month=hint.month, day=hint.day)
                if candidate < hint:
                    return candidate + timedelta(days=1)
                return candidate
            elif "%Y" not in date_format:
                candidate = dt.replace(year=hint.year)
                if candidate < hint:
                    return candidate.replace(year=hint.year + 1)
                return candidate
        else:
            if "%d" not in date_format:
                candidate = dt.replace(
                    year=datetime.now().year,
                    month=datetime.now().month,
                    day=datetime.now().day,
                )
                if candidate > datetime.now().replace(tzinfo=timezone(timedelta(hours=8))):
                    return candidate - timedelta(days=1)
                return candidate
            elif "%Y" not in date_format:
                candidate = dt.replace(year=datetime.now().year)
                if candidate > datetime.now().replace(tzinfo=timezone(timedelta(hours=8))):
                    return candidate.replace(year=datetime.now().year - 1)
                return candidate
        # Return the original timestamp if full date time info is present
        return dt

    @staticmethod
    def get_stamped_log_from_line(line: str, hint: datetime, regex, date_format) -> StampedLog:
        time_candidate = regex.search(line)
        if not time_candidate:
            return StampedLog(None, line)
        try:
            parsed_time = datetime.strptime(time_candidate.group(), date_format).replace(tzinfo=timezone(timedelta(hours=8)))
            return StampedLog(LogReader.get_timestamp_with_hint(parsed_time, hint, date_format), line)
        except ValueError:
            return StampedLog(None, line)

    def analyze_ts_schema(self) -> tuple[re.Pattern, str]:
        """Analyze the timestamp schema of the log file."""
        with self.file_path.open("r", encoding=self.encoding) as f:
            # init queue for each ts schema
            queues = [OrderedQueue(self.buffer_size, self.accepted_order) for _ in TS_SCHEMA]
            counts = [0] * len(TS_SCHEMA)

            # Read line by line not exceeding the chunk size
            while f.tell() < CHUNK_SIZE:
                line = f.readline(CHUNK_SIZE - f.tell())
                if not line:
                    break
                for idx, (regex, date_format) in enumerate(TS_SCHEMA):
                    stamped_log = LogReader.get_stamped_log_from_line(line, self.hint, regex, date_format)
                    cur_log = queues[idx].consume(stamped_log)
                    if cur_log and cur_log.timestamp:
                        counts[idx] += 1

            # Finish reading the file and dump the remaining logs
            for idx, _ in enumerate(TS_SCHEMA):
                counts[idx] += len([x for x in queues[idx].dump_remaining() if x.timestamp])

            # Find the most likely timestamp schema
            max_idx = counts.index(max(counts))
            return TS_SCHEMA[max_idx]

    def get_start_timestamp(self) -> int | None:
        """Get the start timestamp of the log file."""
        with self.file_path.open("r", encoding=self.encoding) as f:
            queue = OrderedQueue(self.buffer_size, self.accepted_order)
            while f.tell() < CHUNK_SIZE:
                line = f.readline(CHUNK_SIZE - f.tell())
                if not line:
                    break

                stamped_log = LogReader.get_stamped_log_from_line(line, self.hint, self.regex, self.date_format)
                cur_log = queue.consume(stamped_log)
                if cur_log and cur_log.timestamp:
                    return int(cur_log.timestamp.timestamp())

            # Finish reading the file and still no timestamp found, dump the remaining logs and get first
            remaining_logs = queue.dump_remaining()
            remaining_stamped_logs = [x for x in remaining_logs if x.timestamp]
            if remaining_stamped_logs:
                return int(remaining_stamped_logs[0].timestamp.timestamp())

            _log.warning(f"Failed to get start timestamp from {self.file_path}")
        return None

    def get_end_timestamp(self) -> int | None:
        """Get the end timestamp of the log file."""
        with self.file_path.open("r", encoding=self.encoding) as f:
            f.seek(0, 2)
            end_offset = f.tell()
            start_offset = max(0, end_offset - CHUNK_SIZE)
            f.seek(start_offset)

            chunk = f.read(CHUNK_SIZE)
            queue = OrderedQueue(self.buffer_size, self.accepted_order)
            for line in reversed(chunk.splitlines()):
                stamped_log = LogReader.get_stamped_log_from_line(line, self.hint, self.regex, self.date_format)
                queue.consume(stamped_log)

            # Get the last timestamp in the queue
            remaining_logs = queue.dump_remaining()
            remaining_stamp_logs = [x for x in remaining_logs if x.timestamp]
            if remaining_stamp_logs:
                return int(remaining_stamp_logs[-1].timestamp.timestamp())

        _log.warning(f"Failed to get end timestamp from {self.file_path}")
        return None

    def stamped_log_generator(self):
        with self.file_path.open("r", encoding=self.encoding) as f:
            queue = OrderedQueue(self.buffer_size, self.accepted_order)
            to_yield = None
            while line := f.readline():
                stamped_log = LogReader.get_stamped_log_from_line(line, self.hint, self.regex, self.date_format)
                cur_log = queue.consume(stamped_log)
                if cur_log:
                    if cur_log.timestamp:
                        if to_yield:
                            yield to_yield
                        to_yield = cur_log
                    else:
                        to_yield.line += cur_log.line

            # Finish reading the file and dump the remaining logs
            remaining_logs = queue.dump_remaining()
            for stamped_log in remaining_logs:
                if stamped_log.timestamp:
                    if to_yield:
                        yield to_yield
                    to_yield = stamped_log
                else:
                    to_yield.line += stamped_log.line

            if to_yield:
                yield to_yield
