package engine

import (
	"testing"

	"agentflow/internal/domain"
)

func TestWebhookTriggerNodeValidates(t *testing.T) {
	wf := domain.WorkflowDefinition{
		ID: "wf-webhook", Name: "Webhook SOP", SandboxProfileID: "test-native",
		Nodes: []domain.WorkflowNode{
			{ID: "start", Name: "From webhook", Type: "webhook", MaxAttempts: 1},
			{ID: "work", Name: "Work", Type: "script", ScriptLanguage: "shell", Script: "true", MaxAttempts: 1},
		},
		Edges: []domain.WorkflowEdge{{Source: "start", Target: "work", When: "success"}},
	}
	if err := validate(wf); err != nil {
		t.Fatalf("validate webhook trigger SOP: %v", err)
	}
	wf.Nodes[0].Type = "webhook_inbound"
	if err := validate(wf); err != nil {
		t.Fatalf("validate webhook_inbound: %v", err)
	}
}
