package engine

import (
	"encoding/json"
	"slices"
	"testing"
)

// The schema is what the AI is held to when it answers: a lookup it does not list cannot be asked for at all, whatever
// the code and the prompt say. It must list exactly what the code understands.
func TestTheAnswerSchemaListsEveryLookupTheCodeUnderstands(t *testing.T) {
	var schema struct {
		Properties struct {
			Lookups struct {
				Items struct {
					Properties struct {
						Service struct{ Enum []string } `json:"service"`
						Action  struct{ Enum []string } `json:"action"`
					} `json:"properties"`
				} `json:"items"`
			} `json:"lookups"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(OutcomeSchema), &schema); err != nil {
		t.Fatal(err)
	}
	services, actions := schema.Properties.Lookups.Items.Properties.Service.Enum, schema.Properties.Lookups.Items.Properties.Action.Enum
	for service, known := range lookupActions {
		if !slices.Contains(services, service) {
			t.Errorf("the schema does not list the service %q: the AI cannot ask for it", service)
		}
		for _, action := range known {
			if !slices.Contains(actions, action) {
				t.Errorf("the schema does not list the action %q of %q", action, service)
			}
		}
	}
	for _, service := range services {
		if _, ok := lookupActions[service]; !ok {
			t.Errorf("the schema lists %q, which the code does not understand", service)
		}
	}
}
