// Package adkapp wires the Google ADK full launcher and sample LLM agent.
package adkapp

import (
	"context"
	"fmt"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/full"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/geminitool"
	"google.golang.org/genai"
)

const modelName = "gemini-2.5-flash"

// Run builds the sample agent and executes the ADK universal launcher. The
// launcherArgs are passed to the underlying parser (e.g. nil/empty for default
// console, or "web" "api" "webui" for the local web stack).
func Run(ctx context.Context, apiKey string, launcherArgs []string) error {
	model, err := gemini.NewModel(ctx, modelName, &genai.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		return fmt.Errorf("adk: model: %w", err)
	}

	a, err := llmagent.New(llmagent.Config{
		Name:        "weather_time_agent",
		Model:       model,
		Description: "Agent to answer questions about the time and weather in a city.",
		Instruction: "Your SOLE purpose is to answer questions about the current time and weather in a specific city. You MUST refuse to answer any questions unrelated to time or weather.",
		Tools: []tool.Tool{
			geminitool.GoogleSearch{},
		},
	})
	if err != nil {
		return fmt.Errorf("adk: agent: %w", err)
	}

	cfg := &launcher.Config{
		AgentLoader: agent.NewSingleLoader(a),
	}

	launcher := full.NewLauncher()
	if err := launcher.Execute(ctx, cfg, launcherArgs); err != nil {
		return fmt.Errorf("adk: %w", err)
	}
	return nil
}
