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

import click

from cos.cli.context import Context
from cos.constant import COS_CACHE_PATH
from cos.core.exceptions import CosException

_log = logging.getLogger(__name__)


@click.group
def remote_config():
    pass


@remote_config.command
@click.pass_obj
def rules(ctx: Context):
    api_state = ctx.api.state.load_state()
    # rules_version = {}
    device_rules = {}

    cache_file = COS_CACHE_PATH / 'rules' / 'rules.json'
    if not cache_file.parent.exists():
        os.makedirs(cache_file.parent)

    if api_state.device is None:
        raise CosException("device is none")

    device_name = api_state.device.get("name", None)
    if device_name is None:
        raise CosException("device_name is none")

    try:
        if os.path.exists(cache_file):
            with open(cache_file, "r", encoding="utf-8") as file:
                device_rules = json.load(file)
    except:
        _log.warning("illegal cache file, ignore it")

    projects = ctx.api.list_device_projects(device_name=device_name)
    project_names = [p.get("name") for p in projects]

    need_update = []
    for project_name in project_names:
        try:
            ver = ctx.api.get_diagnosis_rules_metadata(project_name).get("currentVersion", -1)
            if project_name not in device_rules or device_rules[project_name]["version"] != ver:
                device_rules[project_name] = {"version" : ver}
                need_update.extend(project_names)
        except Exception:
            continue

    if len(need_update) > 0:
        for update_project in need_update:
            try:
                project_rules = ctx.api.get_diagnosis_rule(update_project)
            except Exception:
                continue
            device_rules[update_project]["rules"] = project_rules["rules"]

        with open(cache_file, "w", encoding="utf-8") as f:
            json.dump(device_rules, f, ensure_ascii=False, indent=4)

    click.echo(json.dumps(device_rules))


@remote_config.command
@click.option('--project', type=str, help='rules\r project name')
@click.option('--hit', type=str, help='rules\'s hit')
@click.option('--device', default=None, type=str, help='device')
@click.pass_obj
def trigger_count(ctx: Context, project: str, hit: str, device : str):
    count = ctx.api.count_diagnosis_rules_hit(project, hit, device)
    click.echo(json.dumps(count))


@remote_config.command
@click.option('--project', type=str, help='rules\r project name')
@click.option('--hit', type=str, help='rules\'s hit')
@click.option('--triggered', type=bool, help='rules triggered')
@click.option('--device', default=None, type=str, help='device')
@click.pass_obj
def trigger_rules(ctx: Context, project: str, hit: str, triggered: bool, device : str):
    ctx.api.hit_diagnosis_rule(project, hit, device, triggered)