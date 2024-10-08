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
import subprocess

_log = logging.getLogger(__name__)


def machine_id():
    try:
        output = subprocess.check_output(
            ["dmidecode", "-t", "system"],
            stderr=subprocess.PIPE,
            universal_newlines=True,
            shell=True,
            encoding="utf-8",
        )

        for line in output.splitlines():
            if "UUID:" in line:
                return line.split(":")[1].strip()
    except subprocess.CalledProcessError as e:
        _log.warning(f"Failed to get machine_id: {e.stderr}")
    return None
