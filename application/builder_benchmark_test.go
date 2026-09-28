package application_test

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/application"
	"testing"
)

func BenchmarkConfiguredBuildAndShutdown(b *testing.B) {
	s := settings()
	s.HTTP.Enabled = false
	b.ReportAllocs()
	for b.Loop() {
		app, err := application.New(s, quiet()).Build(context.Background())
		if err != nil {
			b.Fatal(err)
		}
		if err := app.Start(context.Background()); err != nil {
			b.Fatal(err)
		}
		if err := app.Shutdown(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}
