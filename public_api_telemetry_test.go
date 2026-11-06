package foundry_test

import "testing"

// The root source inventory owns discovery. Telemetry adds no private-receiver
// exceptions or mutable storage declarations; keep its documented surface exact.
func TestTelemetryConsumerAPIInventory(t *testing.T) {
	want := map[string]bool{
		"Instance": false, "Release": false, "Portal": false,
		"Source": false, "Rule": false, "AlertState": false,
		"Fingerprint": false, "AlertKey": false, "Episode": false,
		"ParseFingerprint": false, "ParseAlertKey": false,
		"Run": false, "Runtime": false, "Health": false,
		"DependencyHealth": false, "Sample": false, "Frame": false, "Detail": false,
		"RoutePolicy": false, "RoutePolicy.Validate": false,
		"BrowserPolicy": false, "BrowserPolicy.Validate": false,
		"CollectorConfig": false, "CollectorConfig.Validate": false,
		"Thresholds": false, "Thresholds.Validate": false,
		"AlertPolicy": false, "AlertPolicy.Validate": false,
		"RetentionPolicy": false, "RetentionPolicy.Validate": false,
		"ReadLimits": false, "ReadLimits.Validate": false,
		"Config": false, "Config.Validate": false,
		"Migrations": false, "PruneResult": false,
		"Dependencies": false, "Engine": false, "New": false, "KernelModule": false,
		"Engine.Run": false, "Engine.Close": false, "Engine.Done": false, "Engine.Flush": false,
		"Engine.RecordBrowser": false, "Engine.Reader": false, "Engine.RunID": false, "Engine.Prune": false, "Engine.AcknowledgeIn": false,
		"Reader": false, "Reader.Overview": false, "Reader.Traffic": false,
		"Reader.Issue": false, "Reader.Issues": false, "Reader.Events": false, "Reader.Alerts": false,
		"Window": false, "Window.Validate": false,
		"OverviewQuery": false, "TrafficQuery": false, "IssueQuery": false,
		"EventQuery": false, "AlertQuery": false, "Acknowledgement": false,
		"Issue": false, "Event": false, "Alert": false,
		"TrafficRow": false, "TrafficSummary": false, "Traffic": false, "Overview": false,
		"Collector": false, "NewCollector": false,
		"Collector.RunID": false, "Collector.SetRoutes": false,
		"Collector.ObserveRequest": false, "Collector.ObserveSecurityRequest": false,
		"Collector.Report": false, "Collector.RecordBrowser": false,
		"BrowserCode": false, "BrowserFrame": false, "BrowserError": false,
	}
	for _, enum := range []string{"Source", "Rule", "AlertState", "BrowserCode"} {
		for _, method := range []string{"EnumDescriptor", "IsValid", "Validate", "MarshalText", "UnmarshalText", "MarshalJSON", "UnmarshalJSON", "Value", "Scan"} {
			want[enum+"."+method] = false
		}
		want[enum+"Values"] = false
		want[enum+"Codec"] = false
		want["Parse"+enum] = false
	}
	for _, declaration := range consumerAPIDeclarations(t) {
		if declaration.Package != "telemetry" {
			continue
		}
		name := declaration.Name
		if declaration.Receiver != "" {
			name = declaration.Receiver + "." + name
		}
		if _, exists := want[name]; !exists {
			t.Errorf("unexpected telemetry API: %s", name)
			continue
		}
		if !declaration.Documented {
			t.Errorf("telemetry.%s needs documentation", name)
		}
		want[name] = true
	}
	for name, found := range want {
		if !found {
			t.Errorf("telemetry API missing from source inventory: %s", name)
		}
	}
}

func TestTelemetryTestkitConsumerAPIInventory(t *testing.T) {
	found := false
	for _, declaration := range consumerAPIDeclarations(t) {
		if declaration.Package != "testkit/telemetry" {
			continue
		}
		if declaration.Name != "Flush" || declaration.Kind != "func" || !declaration.Documented {
			t.Errorf("unexpected or undocumented telemetry testkit API: %+v", declaration)
		}
		found = true
	}
	if !found {
		t.Fatal("telemetry testkit Flush missing from source inventory")
	}
}
