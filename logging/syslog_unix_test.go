//go:build !windows && !plan9

package logging_test

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/logging"
)

func TestSyslogSinkMapsRecordLevelsToSeverity(t *testing.T) {
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	sink, err := logging.PrepareSink(logging.SinkConfig{Driver: logging.Syslog, Level: -4, Syslog: logging.SyslogConfig{Network: "udp", Address: listener.LocalAddr().String(), Tag: "foundry-test", Facility: logging.FacilityLocal3}})
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	logger := sink.Logger().With("password", "private-value")
	logger.Debug("debug-record")
	logger.Info("info-record")
	logger.Warn("warn-record")
	logger.Error("error-record")
	// local3 is facility 19: priority = 19*8 + severity.
	want := map[string]string{"debug-record": "<159>", "info-record": "<158>", "warn-record": "<156>", "error-record": "<155>"}
	buffer := make([]byte, 64<<10)
	for range 4 {
		if err := listener.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatal(err)
		}
		n, _, err := listener.ReadFrom(buffer)
		if err != nil {
			t.Fatal(err)
		}
		message := string(buffer[:n])
		if strings.Contains(message, "private-value") || !strings.Contains(message, "foundry-test") {
			t.Fatal("syslog record leaked a credential or lost its tag", message)
		}
		for text, priority := range want {
			if strings.Contains(message, text) {
				if !strings.HasPrefix(message, priority) {
					t.Fatal("wrong syslog severity", message)
				}
				delete(want, text)
			}
		}
	}
	if len(want) != 0 {
		t.Fatal("syslog records missing", want)
	}
	if stats := sink.Stats(); stats.Records != 4 || stats.Failures != 0 {
		t.Fatal("syslog records were not counted", stats)
	}
}

func TestSyslogConfigValidation(t *testing.T) {
	for _, config := range []logging.SyslogConfig{{Address: "127.0.0.1:514"}, {Network: "udp"}, {Network: "http", Address: "x"}, {Network: "unix", Address: "relative"}, {Tag: "has space"}, {Facility: "kern"}} {
		if err := (logging.SinkConfig{Driver: logging.Syslog, Syslog: config}).Validate(); err == nil {
			t.Fatal("invalid syslog configuration accepted", config)
		}
	}
	if err := (logging.SinkConfig{Driver: logging.Syslog, Path: "/var/log/app"}).Validate(); err == nil {
		t.Fatal("syslog accepted file options")
	}
	if err := (logging.SinkConfig{Driver: logging.Syslog}).Validate(); err != nil {
		t.Fatal(err)
	}
}
