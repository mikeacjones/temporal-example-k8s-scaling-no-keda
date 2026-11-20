package main

import (
	"testing"
	"time"

	"go.temporal.io/sdk/testsuite"
)

func TestProcessMediaAndRolloutGate(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	for _, gate := range []bool{false, true} {
		env := suite.NewTestWorkflowEnvironment()
		env.RegisterActivity(AnalyzeMedia)
		if gate {
			env.ExecuteWorkflow(RolloutGate)
		} else {
			env.ExecuteWorkflow(ProcessMedia, time.Millisecond)
		}
		if !env.IsWorkflowCompleted() {
			t.Fatal("workflow did not complete")
		}
		if err := env.GetWorkflowError(); err != nil {
			t.Fatal(err)
		}
	}
}
