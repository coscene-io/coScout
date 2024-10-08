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

import click

from cos.cli.context import Context
from cos.install import updater

_log = logging.getLogger(__name__)


@click.command
@click.pass_obj
def update(ctx: Context):
    _log.info("Check for new version...")
    updater.Updater(ctx.conf.updater).run(skip=True)
