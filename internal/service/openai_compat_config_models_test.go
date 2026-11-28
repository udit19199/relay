package service

import (
	"testing"

	"github.com/udit19199/relay/internal/config"
)

func TestBuildOpenAICompatibilityConfigModels_InputModalities(t *testing.T) {
	compat := &config.OpenAICompatibility{
		Name: "mimo",
		Models: []config.OpenAICompatibilityModel{
			{
				Name:            "upstream-vision",
				Alias:           "mimo-v2.5-pro",
				DisplayName:     "Mimo Vision",
				InputModalities: []string{"TEXT", "image", "image"},
			},
		},
	}

	models := buildOpenAICompatibilityConfigModels(compat)
	if len(models) != 1 {
		t.Fatalf("model count = %d, want 1", len(models))
	}

	var vision *ModelInfo
	for _, model := range models {
		if model == nil {
			continue
		}
		switch model.ID {
		case "mimo-v2.5-pro":
			vision = model
		}
	}
	if vision == nil {
		t.Fatal("expected vision model")
	}
	if vision.DisplayName != "Mimo Vision" {
		t.Fatalf("DisplayName = %q, want Mimo Vision", vision.DisplayName)
	}
	if got := joinModalities(vision.SupportedInputModalities); got != "text,image" {
		t.Fatalf("SupportedInputModalities = %q, want text,image", got)
	}
}

func joinModalities(modalities []string) string {
	if len(modalities) == 0 {
		return ""
	}
	out := modalities[0]
	for i := 1; i < len(modalities); i++ {
		out += "," + modalities[i]
	}
	return out
}
