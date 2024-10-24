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

from dataclasses import dataclass

from kebab import KebabSource

from cos.collector.openers import CosHandler
from cos.config import AppConfig
from cos.core.api import ApiClient


@dataclass
class Context:
    config_file: str
    source: KebabSource
    conf: AppConfig
    api: ApiClient
    cos_url_handler: CosHandler
