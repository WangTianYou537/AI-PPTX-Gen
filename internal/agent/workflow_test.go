package agent

import "testing"

func TestDefaultWorkflowHasOutline(t *testing.T) {
	wf := NormalizeWorkflow(Workflow{})
	has := false
	for _, s := range wf.Steps {
		if s.Kind == StepOutline && s.Enabled {
			has = true
		}
	}
	if !has {
		t.Fatal("expected outline step")
	}
}

func TestParseWorkflowJSONEmpty(t *testing.T) {
	wf, err := ParseWorkflowJSON("")
	if err != nil {
		t.Fatal(err)
	}
	if len(wf.Steps) == 0 {
		t.Fatal("expected default steps")
	}
}
