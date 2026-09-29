//go:build !windows && !plan9

package logging

import (
	"log/slog"
	"log/syslog"

	"github.com/weiloon1234/Foundry-Go/fault"
)

const syslogSupported = true

var syslogFacilities = map[SyslogFacility]syslog.Priority{
	"": syslog.LOG_USER, FacilityUser: syslog.LOG_USER, FacilityDaemon: syslog.LOG_DAEMON,
	FacilityLocal0: syslog.LOG_LOCAL0, FacilityLocal1: syslog.LOG_LOCAL1, FacilityLocal2: syslog.LOG_LOCAL2, FacilityLocal3: syslog.LOG_LOCAL3,
	FacilityLocal4: syslog.LOG_LOCAL4, FacilityLocal5: syslog.LOG_LOCAL5, FacilityLocal6: syslog.LOG_LOCAL6, FacilityLocal7: syslog.LOG_LOCAL7,
}

// syslogConnection borrows log/syslog's writer, which serializes its own
// connection and reconnects once after a failed write.
type syslogConnection struct{ writer *syslog.Writer }

func openSyslog(config SyslogConfig) (*syslogConnection, error) {
	writer, err := syslog.Dial(config.Network, config.Address, syslogFacilities[config.Facility]|syslog.LOG_INFO, config.Tag)
	if err != nil {
		return nil, fault.Wrap(fault.Internal, "cannot connect to syslog", err)
	}
	return &syslogConnection{writer: writer}, nil
}

func (c *syslogConnection) writeRecord(data []byte, level slog.Level) (int, error) {
	message := string(data)
	var err error
	switch {
	case level >= slog.LevelError:
		err = c.writer.Err(message)
	case level >= slog.LevelWarn:
		err = c.writer.Warning(message)
	case level >= slog.LevelInfo:
		err = c.writer.Info(message)
	default:
		err = c.writer.Debug(message)
	}
	if err != nil {
		return 0, err
	}
	return len(data), nil
}

func (c *syslogConnection) Close() error { return c.writer.Close() }
