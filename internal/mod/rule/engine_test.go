// Copyright 2026 coScene
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package rule

import (
	"testing"
	"time"

	"buf.build/gen/go/coscene-io/coscene-openapi/protocolbuffers/go/coscene/openapi/dataplatform/v1alpha1/resources"
	"github.com/coscene-io/coscout/pkg/rule_engine"
	mapset "github.com/deckarep/golang-set/v2"
)

func TestUpdateRulesKeepsAllTopicsActiveWhenAnyRuleHasNoTopic(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		rules []*resources.DiagnosisRule
	}{
		{
			name: "wildcard rule first",
			rules: []*resources.DiagnosisRule{
				testDiagnosisRule("wildcard", ""),
				testDiagnosisRule("specific", "/foo"),
			},
		},
		{
			name: "wildcard rule last",
			rules: []*resources.DiagnosisRule{
				testDiagnosisRule("specific", "/foo"),
				testDiagnosisRule("wildcard", ""),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			engine := Engine{}
			engine.UpdateRules(tc.rules, []string{"/foo", "/bar"})

			if got := engine.ActiveTopics().Cardinality(); got != 0 {
				t.Fatalf("active topics cardinality = %d, want 0 so file readers do not prefilter topics", got)
			}
		})
	}
}

func TestUpdateRulesFiltersConfiguredActiveTopics(t *testing.T) {
	t.Parallel()

	engine := Engine{}
	engine.UpdateRules([]*resources.DiagnosisRule{
		testDiagnosisRule("specific", "/foo"),
		testDiagnosisRule("not-configured", "/bar"),
	}, []string{"/foo"})

	activeTopics := engine.ActiveTopics()
	if got := activeTopics.Cardinality(); got != 1 {
		t.Fatalf("active topics cardinality = %d, want 1", got)
	}
	if !activeTopics.Contains("/foo") {
		t.Fatalf("active topics = %v, want /foo", activeTopics)
	}
}

func TestConsumeNextDebouncesEachScopeSeparately(t *testing.T) {
	t.Parallel()

	var triggered []string
	action, err := rule_engine.NewAction("capture", map[string]interface{}{
		"code": "{scope.code}",
	}, func(kwargs map[string]interface{}) error {
		code, ok := kwargs["code"].(string)
		if !ok {
			t.Fatalf("code = %T, want string", kwargs["code"])
		}
		triggered = append(triggered, code)
		return nil
	})
	if err != nil {
		t.Fatalf("new action: %v", err)
	}
	condition, err := rule_engine.NewCondition("msg.code == scope.code")
	if err != nil {
		t.Fatalf("new condition: %v", err)
	}

	baseMetadata := map[string]interface{}{
		"rule_name":         "projects/project-1/diagnosisRuleSets/set-1/diagnosisRules/shared",
		"rule_display_name": "shared",
	}
	engine := Engine{
		ruleDebounceTime: make(map[string]*time.Time),
		rules: []*rule_engine.Rule{
			rule_engine.NewRule(
				[]rule_engine.Condition{*condition},
				[]rule_engine.Action{*action},
				map[string]string{"code": "A"},
				mapset.NewSet[string]("/fault"),
				time.Minute,
				baseMetadata,
			),
			rule_engine.NewRule(
				[]rule_engine.Condition{*condition},
				[]rule_engine.Action{*action},
				map[string]string{"code": "B"},
				mapset.NewSet[string]("/fault"),
				time.Minute,
				baseMetadata,
			),
		},
	}

	engine.ConsumeNext(rule_engine.RuleItem{Msg: map[string]interface{}{"code": "A"}, Topic: "/fault", Ts: 1})
	engine.ConsumeNext(rule_engine.RuleItem{Msg: map[string]interface{}{"code": "B"}, Topic: "/fault", Ts: 2})

	if len(triggered) != 2 {
		t.Fatalf("triggered scopes = %v, want both scopes to trigger independently", triggered)
	}
	if triggered[0] != "A" || triggered[1] != "B" {
		t.Fatalf("triggered scopes = %v, want [A B]", triggered)
	}
}

func testDiagnosisRule(id string, activeTopic string) *resources.DiagnosisRule {
	return &resources.DiagnosisRule{
		Name:             "projects/project-1/diagnosisRuleSets/set-1/diagnosisRules/" + id,
		DisplayName:      id,
		Version:          resources.DiagnosisRule_VERSION_2,
		ActiveTopics:     activeTopic,
		DebounceDuration: "PT0S",
		ConditionSpecs:   []*resources.ConditionSpec{{Condition: &resources.ConditionSpec_Raw{Raw: "true"}}},
		ActionSpecs:      []*resources.ActionSpec{{Spec: &resources.ActionSpec_Upload{Upload: &resources.UploadActionSpec{PreTrigger: "PT1S", PostTrigger: "PT1S"}}}},
	}
}
