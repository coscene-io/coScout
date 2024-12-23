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

import celpy
import json
import logging
from collections import namedtuple
from datetime import datetime, timedelta
from functools import partial

from rule_engine.rule import Rule, validate_rule_spec
from ruleengine.dsl.base_actions import noop_create_moment
from ruleengine.dsl.validation.config_validator import validate_config_wrapped
from ruleengine.engine import Rule as V1Rule

from cos.core.api import ApiClient
from cos.mods.common.default.remote_rule import RemoteRule

_log = logging.getLogger(__name__)

RuleDataItem = namedtuple("RuleDataItem", "topic msg ts msgtype")


class CompatibleEngine:
    def __init__(self, v1_rules: list[V1Rule], v2_rules: list[Rule], api_client: ApiClient, device: str):
        self.v1_rules = v1_rules
        self.v2_rules = v2_rules
        self.api_client = api_client
        self.device = device

    def consume_next(self, item: RuleDataItem):
        """
        Consume a message and evaluate upon all rules
        msg_fn: a factory function that returns a message using msg_fn()
        """

        activation_without_scope = {
            "msg": celpy.adapter.json_to_cel(item.msg),
            "topic": celpy.celtypes.StringType(item.topic),
            "ts": celpy.celtypes.DoubleType(item.ts),
        }

        for rule in self.v1_rules:
            triggered_condition_indices = []
            triggered_scope = None

            for i, cond in enumerate(rule.conditions):
                res, scope = cond.evaluate_condition_at(item, rule.initial_scope)
                _log.debug(f"evaluate condition, result: {res}, scope: {scope}")
                if res:
                    triggered_condition_indices.append(i)
                if not triggered_scope:
                    triggered_scope = scope

            if not triggered_condition_indices:
                continue

            # For testing, rule.spec is not specified
            hit = (
                {}
                if not rule.spec
                else {
                    **rule.spec,
                    "when": [rule.spec["when"][i] for i in triggered_condition_indices],
                }
            )

            should_upload = self.should_upload(rule.project_name, rule.spec, hit)
            if should_upload:
                for action in rule.actions:
                    action.run(item, triggered_scope)

            self.hit_upload(rule.project_name, rule.spec, hit, should_upload)

        for rule in self.v2_rules:
            if rule.topics and item.topic not in rule.topics:
                continue
            activation = {**activation_without_scope, "scope": celpy.adapter.json_to_cel(rule.scope)}

            _log.info(f"==> Evaluating rule with debounce time {rule.debounce_time}")
            if not rule.eval_conditions(activation, item.ts):
                continue

            should_upload = self.should_upload(
                rule.metadata["project_name"], rule.metadata["original"], rule.metadata["original"]
            )
            if should_upload:
                for action in rule.actions:
                    action.run(activation)

            self.hit_upload(rule.metadata["project_name"], rule.metadata["original"], rule.metadata["original"], should_upload)

    def should_upload(self, proj_name, rule_spec, hit):
        if not hit.get("uploadLimit", ""):
            return True

        project_rule_spec = {
            "name": f"{proj_name}/diagnosisRule",
            "rules": [
                {
                    "rules": [rule_spec],
                }
            ],
        }

        upload_limit = hit["uploadLimit"]
        if upload_limit.get("device", ""):
            try:
                device_count = self.api_client.count_diagnosis_rules_hit(project_rule_spec["name"], hit, self.device)["count"]
            except Exception:
                _log.warning(f"==> Failed to count device hit for {project_rule_spec['name']}, skipping")
                return False
            if device_count >= upload_limit["device"]["times"]:
                _log.info(
                    f"device count {device_count} reached upload limit {upload_limit['device']['times']} times, skipping"
                )
                return False

        if upload_limit.get("global", ""):
            try:
                global_count = self.api_client.count_diagnosis_rules_hit(project_rule_spec["name"], hit, "")["count"]
            except Exception:
                _log.warning(f"==> Failed to count global hit for {project_rule_spec['name']}, skipping")
                return False
            if global_count >= upload_limit["global"]["times"]:
                _log.info(
                    f"global count {global_count} reached upload limit {upload_limit['global']['times']} times, skipping"
                )
                return False

        return True

    def hit_upload(self, proj_name, rule_spec, hit, upload_triggered):
        project_rule_spec = {
            "name": f"{proj_name}/diagnosisRule",
            "rules": [
                {
                    "rules": [rule_spec],
                }
            ],
        }
        try:
            self.api_client.hit_diagnosis_rule(project_rule_spec, hit, self.device, upload_triggered)
        except Exception:
            _log.warning(f"==> Failed to hit diagnosis rule for {project_rule_spec['name']}, skipping")
            pass


class RuleExecutor:
    def __init__(self, name, api_client: ApiClient, upload_fn):
        self.__name = name
        self.__api_client = api_client
        self.__remote_rule = RemoteRule(api_client)
        self.__upload_fn = upload_fn
        self.active_topics = set()
        self.__device = self.__api_client.state.load_state().device.get("name", "")
        self.__configs = None
        self.__engine: CompatibleEngine | None = None
        self.__update_engine()

    def execute(self, input_stream):
        _log.info(f"==> {self.__name} consume_chunk started")
        start_time = datetime.now()
        last_item_read_time = start_time
        for item in input_stream:
            # This is to avoid gap in input stream
            if datetime.now() - last_item_read_time > timedelta(seconds=30):
                self.__update_engine()
            self.__engine.consume_next(item)
            if datetime.now() - start_time > timedelta(minutes=1):
                self.__update_engine()
                start_time = datetime.now()
            last_item_read_time = datetime.now()
        _log.info(f"==> {self.__name} consume_chunk ended")

    def __update_engine(self):
        new_configs = self.__remote_rule.list_device_diagnosis_rules()
        if new_configs == self.__configs:
            return
        self.__configs = new_configs

        v1_rule_list = []
        v2_rule_list = []
        active_topics = set()
        active_topics.add("/external_log")
        all_rules_have_topics = True
        for project_rule_sets in self.__configs:
            if not project_rule_sets.get("name", "").endswith("/diagnosisRule"):
                _log.warning("==> Found an invalid project rule set, skipping")
                continue
            project_name = project_rule_sets["name"].removesuffix("/diagnosisRule")
            for project_rule_set in project_rule_sets["rules"]:
                if not project_rule_set.get("enabled", False):
                    continue

                if project_rule_set.get("version", "") == "v1":
                    validation_result, rules = validate_config_wrapped(
                        project_rule_set,
                        {
                            "upload": lambda rule: partial(self.__upload_fn, project_name=project_name, rule=rule),
                            "create_moment": lambda _: noop_create_moment,
                        },
                        project_name,
                    )
                    if not validation_result["success"]:
                        _log.error(
                            f"==> Failed to build rule for {project_name} "
                            f"{json.dumps(project_rule_set, indent=2, ensure_ascii=False)} "
                            f"due to {json.dumps(validation_result, indent=2, ensure_ascii=False)}, skipping"
                        )
                        continue
                    v1_rule_list += rules
                    if all_rules_have_topics:
                        for v1_rule in project_rule_set.get("rules", []):
                            if not v1_rule.get("activeTopics", []):
                                all_rules_have_topics = False
                                _log.warning(f"no topics found in some rules of {project_name}, will diagnose all topics")
                                active_topics.clear()
                                break
                            else:
                                active_topics.update(v1_rule.get("activeTopics", []))

                elif project_rule_set.get("version", "") == "v2":
                    rules, errs = self.__v2_spec_to_rules(project_rule_set, project_name)
                    if errs:
                        _log.error(
                            f"==> Failed to build rule for {project_name} "
                            f"{json.dumps(project_rule_set, indent=2, ensure_ascii=False)} due to "
                            f"due to {errs}, skipping"
                        )
                        continue
                    v2_rule_list += rules
                    if all_rules_have_topics:
                        for v2_rule in rules:
                            if not v2_rule.topics:
                                _log.warning(f"no topics found in some rules of {project_name}, will diagnose all topics")
                                all_rules_have_topics = False
                                active_topics.clear()
                                break
                            else:
                                active_topics.update(v2_rule.topics)

                else:
                    _log.error(
                        f"==> Found an invalid project rule set version for {project_name} "
                        f"{json.dumps(project_rule_set, indent=2, ensure_ascii=False)}, skipping"
                    )

        self.active_topics = active_topics
        self.__engine = CompatibleEngine(v1_rule_list, v2_rule_list, self.__api_client, self.__device)

    def __v2_spec_to_rules(self, project_rules_spec: dict, project_name: str):
        """Convert a v2 rule spec to a list of Rule objects"""
        rules = []
        errs = []
        for rule_idx, rule_spec in enumerate(project_rules_spec.get("rules", [])):
            conditions = []
            for condition_spec in rule_spec.get("conditionSpecs", []):
                if "raw" in condition_spec:
                    conditions.append(condition_spec["raw"])
                elif "structured" in condition_spec:
                    structured_condition = condition_spec["structured"]
                    sc_type = structured_condition["type"]
                    sc_path = structured_condition["path"]
                    sc_op = structured_condition["op"]

                    # change type from api enum to cel type
                    if sc_type == "STRING":
                        sc_type = "string"
                    elif sc_type == "INT":
                        sc_type = "int"
                    else:
                        sc_type = ""

                    # change op from api enum to cel operator
                    if sc_op == "CONTAINS":
                        sc_op = "contains"
                    if sc_op == "EQUAL":
                        sc_op = "=="
                    else:
                        sc_op = ""

                    if "predefined" in structured_condition:
                        sc_value = structured_condition["predefined"]
                    elif "userInput" in structured_condition:
                        # json marshal the string
                        sc_value = json.dumps(structured_condition["userInput"])
                    else:
                        sc_value = ""

                    # special handle for contains
                    if sc_op == "contains":
                        conditions.append(
                            "{c_type}({c_path}).contains({c_type}({c_value}))".format(
                                c_type=sc_type,
                                c_path=sc_path,
                                c_value=sc_value,
                            )
                        )
                    else:
                        conditions.append(
                            "{c_type}({c_path}) {c_op} {c_type}({c_value})".format(
                                c_type=sc_type,
                                c_path=sc_path,
                                c_op=sc_op,
                                c_value=sc_value,
                            )
                        )

            actions = []
            for action_spec in rule_spec.get("actionSpecs", []):
                if "upload" in action_spec:
                    upload_spec = action_spec["upload"]
                    actions.append(
                        {
                            "name": "upload",
                            "kwargs": {
                                "trigger_ts": """{ts}""",
                                "before": upload_spec.get("preTrigger"),
                                "after": upload_spec.get("postTrigger"),
                                "title": upload_spec.get("title"),
                                "description": upload_spec.get("description"),
                                "labels": upload_spec.get("labels"),
                                "extra_files": upload_spec.get("extraFiles"),
                                "white_list": upload_spec.get("whiteList"),
                            },
                        }
                    )

            # Skip rules without upload action
            if not actions:
                continue

            cur_rules, cur_errs = validate_rule_spec(
                {
                    "conditions": conditions,
                    "actions": actions,
                    "scopes": rule_spec.get("each", []),
                    "topics": rule_spec.get("activeTopics", []),
                    "condition_debounce": rule_spec.get("conditionDebounce", 0),
                },
                {"upload": partial(self.__upload_fn, rule=rule_spec, project_name=project_name)},
                rule_idx,
            )

            for rule in cur_rules:
                # add metadata to the rule
                rule.metadata = {"project_name": project_name, "original": rule_spec}

                # add raw to the rule
                rule.metadata["original"]["each"] = [rule.scope]

            rules.extend(cur_rules)
            errs.extend(cur_errs)
        return rules, errs
