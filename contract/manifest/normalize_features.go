package manifest

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
	"github.com/weiloon1234/Foundry-Go/notifications"
	"github.com/weiloon1234/Foundry-Go/websocket"
)

func normalizeFeatures(d *Document, types typeIndex) error {
	if len(d.Enums) > MaxOperations || len(d.Permissions) > MaxOperations || len(d.Notifications) > MaxOperations || len(d.Tables) > MaxOperations {
		return invalid("feature catalogue is too large")
	}
	enums := make(map[string]bool)
	for i, definition := range d.Enums {
		id := definition.PackagePath + "." + definition.Name
		if enums[id] {
			return invalid("duplicate enum definition")
		}
		enums[id] = true
		normalized, err := normalizeEnum(definition)
		if err != nil {
			return err
		}
		if typ, found := types[contract.TypeID(id)]; found {
			values := make([]string, 0, len(normalized.Cases))
			for _, c := range normalized.Cases {
				values = append(values, string(c.Value))
			}
			slices.Sort(values)
			actual := make([]string, 0, len(typ.Cases))
			for _, c := range typ.Cases {
				actual = append(actual, string(c))
			}
			slices.Sort(actual)
			if !slices.Equal(values, actual) {
				return invalid("enum metadata differs from its wire schema")
			}
		}
		d.Enums[i] = normalized
	}
	slices.SortFunc(d.Enums, func(a, b enum.Definition) int {
		return strings.Compare(a.PackagePath+"."+a.Name, b.PackagePath+"."+b.Name)
	})
	permissions := make(map[auth.PermissionName]bool)
	for _, p := range d.Permissions {
		if !identifier.Semantic(string(p.Name)) || permissions[p.Name] || p.LabelKey != "" && p.LabelKey.Validate() != nil {
			return invalid("invalid or duplicate permission metadata")
		}
		permissions[p.Name] = true
	}
	slices.SortFunc(d.Permissions, func(a, b auth.PermissionDescription) int { return cmp.Compare(a.Name, b.Name) })
	if l := d.Locales; l != nil {
		set, err := i18n.NewLocaleSet(l.Default, l.Supported...)
		if err != nil {
			return err
		}
		l.Supported = set.Locales()
		if len(l.Messages) > i18n.MaxMessages {
			return invalid("too many message definitions")
		}
		keys := make(map[i18n.MessageKey]bool)
		for _, message := range l.Messages {
			if message.Validate() != nil || keys[message.Key] {
				return invalid("invalid or duplicate message definition")
			}
			keys[message.Key] = true
		}
		slices.SortFunc(l.Messages, func(a, b i18n.MessageDefinition) int { return cmp.Compare(a.Key, b.Key) })
	}
	if err := normalizeNotifications(d, types); err != nil {
		return err
	}
	seen := make(map[datatable.TableID]bool)
	for i, table := range d.Tables {
		if seen[table.ID] || !types.has(table.Row) || !types.has(table.Request) {
			return invalid("invalid table schema reference")
		}
		seen[table.ID] = true
		normalized, err := (datatable.Description{ID: table.ID, Row: contract.Schema{Root: table.Row, Types: d.Types}, Request: contract.Schema{Root: table.Request, Types: d.Types}, Columns: table.Columns, Filters: table.Filters, DefaultSort: table.DefaultSort, Exports: table.Exports}).Normalize()
		if err != nil {
			return err
		}
		d.Tables[i].Columns, d.Tables[i].Filters, d.Tables[i].DefaultSort = normalized.Columns, normalized.Filters, normalized.DefaultSort
	}
	slices.SortFunc(d.Tables, func(a, b Table) int { return cmp.Compare(a.ID, b.ID) })
	return nil
}

func normalizeNotifications(d *Document, types typeIndex) error {
	events := make(map[[2]string]Event)
	if d.Realtime != nil {
		for _, channel := range d.Realtime.Channels {
			for _, event := range channel.Events {
				events[[2]string{string(channel.ID), string(event.ID)}] = event
			}
		}
	}
	seen := make(map[string]bool)
	for i := range d.Notifications {
		n := &d.Notifications[i]
		identity := fmt.Sprintf("%s/%s/%d", n.Recipient, n.Name, n.Version)
		if !identifier.Semantic(string(n.Recipient)) || !identifier.Semantic(string(n.Name)) || n.Version == 0 || seen[identity] || len(n.Channels) == 0 || len(n.Channels) > notifications.MaxChannels {
			return invalid("invalid or duplicate notification")
		}
		seen[identity] = true
		channels := make(map[notifications.ChannelID]bool)
		for _, channel := range n.Channels {
			if !identifier.Semantic(string(channel.ID)) || channels[channel.ID] || !types.has(channel.Payload) {
				return invalid("invalid notification channel")
			}
			channels[channel.ID] = true
			switch channel.Kind {
			case "database":
				if channel.Realtime != nil {
					return invalid("inbox metadata claims a realtime event")
				}
			case "realtime":
				if channel.Realtime == nil {
					return invalid("notification has no realtime binding")
				}
				event, found := events[[2]string{string(channel.Realtime.Channel), string(channel.Realtime.Event)}]
				if !found || event.Direction != websocket.ServerToClient || event.Payload != channel.Payload {
					return invalid("notification does not match a registered outgoing event")
				}
			default:
				return invalid("private notification transport cannot become a client contract")
			}
		}
		slices.SortFunc(n.Channels, func(a, b NotificationChannel) int { return cmp.Compare(a.ID, b.ID) })
	}
	slices.SortFunc(d.Notifications, func(a, b Notification) int {
		if order := cmp.Compare(a.Recipient, b.Recipient); order != 0 {
			return order
		}
		if order := cmp.Compare(a.Name, b.Name); order != 0 {
			return order
		}
		return cmp.Compare(a.Version, b.Version)
	})
	return nil
}

func normalizeEnum(d enum.Definition) (enum.Definition, error) {
	if len(d.Cases) == 0 || len(d.Cases) > jsonwire.MaxNodes {
		return enum.Definition{}, invalid("invalid enum size")
	}
	kind, signed := "", false
	for _, c := range d.Cases {
		value, err := jsonwire.Decode(c.Value, jsonwire.Limits{Bytes: jsonwire.MaxBytes, Depth: 0, Nodes: 1})
		if err != nil {
			return enum.Definition{}, err
		}
		current := ""
		switch v := value.(type) {
		case string:
			current = "string"
		case json.Number:
			current = "integer"
			signed = signed || strings.HasPrefix(string(v), "-")
		default:
			return enum.Definition{}, invalid("enum case is not a string or integer")
		}
		if kind != "" && current != kind {
			return enum.Definition{}, invalid("enum mixes scalar kinds")
		}
		kind = current
	}
	if kind == "string" {
		return normalizeEnumCases[string](d)
	}
	if signed {
		return normalizeEnumCases[int64](d)
	}
	return normalizeEnumCases[uint64](d)
}

func normalizeEnumCases[T enum.Scalar](d enum.Definition) (enum.Definition, error) {
	cases := make([]enum.Case[T], 0, len(d.Cases))
	for _, c := range d.Cases {
		var value T
		if err := json.Unmarshal(c.Value, &value); err != nil {
			return enum.Definition{}, invalid("invalid enum scalar value")
		}
		cases = append(cases, enum.Case[T]{Name: c.Name, Value: value, LabelKey: c.LabelKey})
	}
	return enum.Describe(d.PackagePath, d.Name, cases...).Definition()
}
