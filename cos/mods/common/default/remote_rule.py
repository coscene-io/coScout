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

from cos.collector.remote_config import RemoteConfig
from cos.core.api import ApiClient

_log = logging.getLogger(__name__)


class ProjectRemoteRule(RemoteConfig):
    def __init__(self, api_client: ApiClient, project_name: str):
        self._api_client = api_client
        self._project_name = project_name

        super().__init__(enable_cache=True)

    def get_cache_key(self):
        return f"{self._project_name}/diagnosisRules"

    def get_config_version(self):
        return self._api_client.get_diagnosis_rules_metadata(parent_name=self._project_name).get("currentVersion", -1)

    def get_config(self):
        return self._api_client.get_diagnosis_rule(parent_name=self._project_name)


class RemoteRule:
    def __init__(self, api_client: ApiClient):
        self._api_client = api_client

    def get_project_diagnosis_rules(self) -> dict:
        api_state = self._api_client.state.load_state()
        device_name = api_state.device.get("name", None)
        if not device_name:
            _log.warning("device name is not found, skip get project diagnosis rules")
            return {}

        projects = self._api_client.list_device_projects(device_name=device_name)
        if not projects or len(projects) == 0:
            _log.warning("no projects found, skip get project diagnosis rules")
            return {}

        project_rules = {}
        for project in projects:
            project_name = project.get("name")
            remote_rule = ProjectRemoteRule(self._api_client, project_name)

            project_rules[project_name] = remote_rule.read_config()
            project_rules[project_name]["version"] = remote_rule.get_config_version()

        return project_rules

    def list_device_diagnosis_rules(self) -> list:
        api_state = self._api_client.state.load_state()
        device_name = api_state.device.get("name", None)
        if not device_name:
            _log.warning("device name is not found, skip list device diagnosis rules")
            return []

        projects = self._api_client.list_device_projects(device_name=device_name)
        if not projects or len(projects) == 0:
            _log.warning("no projects found, skip list device diagnosis rules")
            return []

        select_rules = []
        for project in projects:
            project_name = project.get("name")
            project_rules = ProjectRemoteRule(self._api_client, project_name).read_config()
            if project_rules:
                select_rules.append(project_rules)

        return select_rules

    def list_topics_in_rules(self) -> list:
        topics = []
        for project_rule_sets in self.list_device_diagnosis_rules():
            for project_rule_set in project_rule_sets.get("rules"):
                if not project_rule_set.get("enabled", False):
                    continue
                for rule_spec in project_rule_set.get("rules"):
                    if not rule_spec.get("activeTopics", []):
                        _log.warning(
                            f"no topics found in rule {rule_spec.get('id', '')} of "
                            + f"{project_rule_sets.get('name')}, will diagnose all topics"
                        )
                        return []
                    topics += rule_spec.get("activeTopics")
        _log.info(f"Active topics in rules: {topics}")
        return topics
