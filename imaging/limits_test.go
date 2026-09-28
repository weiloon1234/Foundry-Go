package imaging

import "testing"

func TestFillRejectsOversizedIntermediateEvenWhenFinalCanvasFits(t *testing.T) {
	config := DefaultConfig()
	config.Limits.Width = 64
	config.Limits.Height = 64
	config.Limits.Pixels = 4096
	engine := testEngine(t, config)
	if _, err := engine.ProcessBytes(t.Context(), pngInput(t, 40, 1), NewPlan().Fill(2, 2, true)); err == nil {
		t.Fatal("fill exceeded admitted intermediate width")
	}
}
