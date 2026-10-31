package application

import (
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/logging"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"maps"
)

// LogChannels maps channel names to logging settings.
type LogChannels map[logging.ChannelName]logging.ChannelSettings

// UnmarshalText decodes the channel table and replaces the receiver only on success.
func (m *LogChannels) UnmarshalText(data []byte) error {
	schema, err := logging.ChannelSettingsConfigSchema()
	if err != nil {
		return err
	}
	values, err := config.DecodeTable(string(data), schema, func(logging.ChannelName) logging.ChannelSettings { return logging.DefaultChannelSettings() }, nil)
	if err != nil {
		return err
	}
	*m = values
	return nil
}

// LogSettings configures named logging channels and their default.
type LogSettings struct {
	Default  logging.ChannelName
	Channels LogChannels
}

// DefaultLogSettings returns logging settings with a single default channel.
func DefaultLogSettings() LogSettings {
	return LogSettings{Default: "default", Channels: LogChannels{"default": logging.DefaultChannelSettings()}}
}

func (s LogSettings) inTimeZone(zone temporal.ZoneName) LogChannels {
	channels := maps.Clone(s.Channels)
	for name, channel := range channels {
		if channel.Sink.Driver != logging.Stack && channel.Sink.Driver != logging.Custom && channel.Sink.TimeZone == "" {
			channel.Sink.TimeZone = zone
			channels[name] = channel
		}
	}
	return channels
}
