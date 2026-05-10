package prompts

import (
	"testing"

	codingagents "github.com/spacelions/j/internal/coding-agents"
	"github.com/spacelions/j/internal/store/tasks"
)

func TestPlanPromptDispatch(t *testing.T) {
	base := codingagents.PlanRequest{
		FromFilePath:           "req.md",
		RequirementsOutputPath: "requirements.md",
		PlanOutputPath:         "plan.md",
		ClarificationPath:      "clarification.md",
		MustRead:               []string{"AGENTS.md"},
	}
	tests := []struct {
		name   string
		mutate func(*codingagents.PlanRequest)
		want   string
	}{
		{
			name: "fresh",
			want: AppendPlannerSaveSuffix(
				BuildPlanner("req.md", []string{"AGENTS.md"}),
				tasks.TaskPaths{
					Requirements:  "requirements.md",
					Plan:          "plan.md",
					Clarification: "clarification.md",
				},
			),
		},
		{
			name:   "resume",
			mutate: func(req *codingagents.PlanRequest) { req.Resume = true },
			want: AppendPlannerSaveSuffix(
				BuildPlannerResume("req.md", []string{"AGENTS.md"}),
				tasks.TaskPaths{
					Requirements:  "requirements.md",
					Plan:          "plan.md",
					Clarification: "clarification.md",
				},
			),
		},
		{
			name: "clarification resume wins",
			mutate: func(req *codingagents.PlanRequest) {
				req.Resume = true
				req.ResumeFromClarification = true
			},
			want: AppendPlannerSaveSuffix(
				BuildPlannerClarificationResume(
					"req.md", "clarification.md", []string{"AGENTS.md"},
				),
				tasks.TaskPaths{
					Requirements:  "requirements.md",
					Plan:          "plan.md",
					Clarification: "clarification.md",
				},
			),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := base
			if tt.mutate != nil {
				tt.mutate(&req)
			}
			if got := PlanPrompt(req); got != tt.want {
				t.Fatalf("PlanPrompt() mismatch\nwant: %q\n got: %q", tt.want, got)
			}
		})
	}
}

func TestWorkPromptDispatch(t *testing.T) {
	base := codingagents.WorkRequest{
		PlanPath:          "plan.md",
		Worktree:          "j-task",
		ClarificationPath: "clarification.md",
		MustRead:          []string{"AGENTS.md"},
	}
	tests := []struct {
		name   string
		mutate func(*codingagents.WorkRequest)
		want   string
	}{
		{
			name: "fresh",
			want: BuildWorker(
				"plan.md", "j-task", []string{"AGENTS.md"},
				"clarification.md",
			),
		},
		{
			name: "resume",
			mutate: func(req *codingagents.WorkRequest) {
				req.Resume = true
			},
			want: BuildWorkerResume(
				"plan.md", "j-task", []string{"AGENTS.md"},
				"clarification.md",
			),
		},
		{
			name: "clarification resume wins over resume",
			mutate: func(req *codingagents.WorkRequest) {
				req.Resume = true
				req.ResumeFromClarification = true
			},
			want: BuildWorkerClarificationResume(
				"plan.md", "j-task", []string{"AGENTS.md"},
				"clarification.md",
			),
		},
		{
			name: "fix findings wins",
			mutate: func(req *codingagents.WorkRequest) {
				req.Resume = true
				req.ResumeFromClarification = true
				req.FixFindings = true
				req.VerifierFindingsOutputPath = "findings.md"
			},
			want: BuildVerifierFix(
				"plan.md", "findings.md", "j-task", "clarification.md",
			),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := base
			if tt.mutate != nil {
				tt.mutate(&req)
			}
			if got := WorkPrompt(req); got != tt.want {
				t.Fatalf("WorkPrompt() mismatch\nwant: %q\n got: %q", tt.want, got)
			}
		})
	}
}

func TestVerifyPromptDispatch(t *testing.T) {
	base := codingagents.VerifyRequest{
		RequirementsPath:           "requirements.md",
		PlanPath:                   "plan.md",
		VerifierPlanOutputPath:     "verifier_plan.md",
		VerifierFindingsOutputPath: "verifier_findings.md",
		Worktree:                   "j-task",
		ClarificationPath:          "clarification.md",
		MustRead:                   []string{"AGENTS.md"},
	}
	tests := []struct {
		name   string
		mutate func(*codingagents.VerifyRequest)
		want   string
	}{
		{
			name: "fresh",
			want: BuildVerifier(
				"requirements.md", "plan.md", "verifier_plan.md",
				"verifier_findings.md", "j-task", []string{"AGENTS.md"},
				"clarification.md",
			),
		},
		{
			name: "resume",
			mutate: func(req *codingagents.VerifyRequest) {
				req.Resume = true
			},
			want: BuildVerifierResume(
				"requirements.md", "plan.md", "j-task",
				[]string{"AGENTS.md"}, "clarification.md",
			),
		},
		{
			name: "clarification resume wins over resume",
			mutate: func(req *codingagents.VerifyRequest) {
				req.Resume = true
				req.ResumeFromClarification = true
			},
			want: BuildVerifierClarificationResume(
				"requirements.md", "plan.md", "j-task",
				[]string{"AGENTS.md"}, "clarification.md",
			),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := base
			if tt.mutate != nil {
				tt.mutate(&req)
			}
			if got := VerifyPrompt(req); got != tt.want {
				t.Fatalf("VerifyPrompt() mismatch\nwant: %q\n got: %q",
					tt.want, got)
			}
		})
	}
}
