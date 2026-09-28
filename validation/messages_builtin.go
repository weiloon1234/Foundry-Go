package validation

import (
	"encoding/json"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"strings"
)

type builtinMessage struct {
	id       RuleID
	text     string
	argument string
	kind     i18n.ParameterKind
	one      string
}

// Private immutable definitions are shared by execution, catalog registration
// and exported recipe metadata. Constructors contain no duplicate message text.
var builtinMessages = [...]builtinMessage{
	{"foundry.absent", "{{attribute}} must not be supplied.", "", "", ""},
	{"foundry.accepted", "{{attribute}} must be accepted.", "", "", ""},
	{"foundry.after", "{{attribute}} must be after {{other}}.", "other", "text", ""},
	{"foundry.after_or_equal", "{{attribute}} must be at or after {{other}}.", "other", "text", ""},
	{"foundry.alpha", "{{attribute}} must contain only letters.", "", "", ""},
	{"foundry.alpha_dash", "{{attribute}} must contain only letters, numbers, dashes and underscores.", "", "", ""},
	{"foundry.alpha_numeric", "{{attribute}} must contain only letters and numbers.", "", "", ""},
	{"foundry.ascii", "{{attribute}} must contain only ASCII characters.", "", "", ""},
	{"foundry.before", "{{attribute}} must be before {{other}}.", "other", "text", ""},
	{"foundry.before_or_equal", "{{attribute}} must be at or before {{other}}.", "other", "text", ""},
	{"foundry.contains", "{{attribute}} must contain the required text.", "", "", ""},
	{"foundry.contains_items", "{{attribute}} is missing required items.", "", "", ""},
	{"foundry.date", "{{attribute}} must be a date.", "", "", ""},
	{"foundry.datetime", "{{attribute}} must be a date and time with an offset.", "", "", ""},
	{"foundry.decimal_max", "{{attribute}} must be at most {{max}}.", "max", "text", ""},
	{"foundry.decimal_min", "{{attribute}} must be at least {{min}}.", "min", "text", ""},
	{"foundry.declined", "{{attribute}} must be declined.", "", "", ""},
	{"foundry.different", "{{attribute}} must differ from {{other}}.", "other", "text", ""},
	{"foundry.digits", "{{attribute}} must contain only digits.", "", "", ""},
	{"foundry.distinct", "{{attribute}} must contain distinct values.", "", "", ""},
	{"foundry.distinct_by", "{{attribute}} must contain distinct item keys.", "", "", ""},
	{"foundry.doesnt_contain", "{{attribute}} contains prohibited text.", "", "", ""},
	{"foundry.doesnt_end_with", "{{attribute}} has a prohibited suffix.", "", "", ""},
	{"foundry.doesnt_start_with", "{{attribute}} has a prohibited prefix.", "", "", ""},
	{"foundry.email", "{{attribute}} must be an email address.", "", "", ""},
	{"foundry.empty", "{{attribute}} must be empty.", "", "", ""},
	{"foundry.ends_with", "{{attribute}} has an invalid suffix.", "", "", ""},
	{"foundry.enum", "{{attribute}} is not a declared enum value.", "", "", ""},
	{"foundry.excludes_items", "{{attribute}} contains prohibited items.", "", "", ""},
	{"foundry.exists", "{{attribute}} has an invalid selection.", "", "", ""},
	{"foundry.exists_all", "{{attribute}} contains one or more invalid selections.", "", "", ""},
	{"foundry.file_content_types", "{{attribute}} has an unsupported content type.", "", "", ""},
	{"foundry.file_extensions", "{{attribute}} has an unsupported filename extension.", "", "", ""},
	{"foundry.file_max_size", "{{attribute}} must contain at most {{bytes}} bytes.", "bytes", "number", "{{attribute}} must contain at most {{bytes}} byte."},
	{"foundry.file_min_size", "{{attribute}} must contain at least {{bytes}} bytes.", "bytes", "number", "{{attribute}} must contain at least {{bytes}} byte."},
	{"foundry.file_present", "{{attribute}} must contain an uploaded file.", "", "", ""},
	{"foundry.greater_or_equal", "{{attribute}} must be greater than or equal to {{other}}.", "other", "text", ""},
	{"foundry.greater_than", "{{attribute}} must be greater than {{other}}.", "other", "text", ""},
	{"foundry.hex_color", "{{attribute}} must be a hexadecimal color.", "", "", ""},
	{"foundry.ip", "{{attribute}} must be an IP address.", "", "", ""},
	{"foundry.ipv4", "{{attribute}} must be an IPv4 address.", "", "", ""},
	{"foundry.ipv6", "{{attribute}} must be an IPv6 address.", "", "", ""},
	{"foundry.json", "{{attribute}} must contain valid JSON.", "", "", ""},
	{"foundry.less_or_equal", "{{attribute}} must be less than or equal to {{other}}.", "other", "text", ""},
	{"foundry.less_than", "{{attribute}} must be less than {{other}}.", "other", "text", ""},
	{"foundry.local_datetime", "{{attribute}} must be a local date and time.", "", "", ""},
	{"foundry.lowercase", "{{attribute}} must be lowercase.", "", "", ""},
	{"foundry.mac_address", "{{attribute}} must be a MAC address.", "", "", ""},
	{"foundry.matches", "{{attribute}} has an invalid format.", "", "", ""},
	{"foundry.max", "{{attribute}} must be at most {{max}}.", "max", "text", ""},
	{"foundry.max_items", "{{attribute}} must contain at most {{max}} items.", "max", "number", "{{attribute}} must contain at most {{max}} item."},
	{"foundry.max_length", "{{attribute}} must contain at most {{max}} characters.", "max", "number", "{{attribute}} must contain at most {{max}} character."},
	{"foundry.min", "{{attribute}} must be at least {{min}}.", "min", "text", ""},
	{"foundry.min_items", "{{attribute}} must contain at least {{min}} items.", "min", "number", "{{attribute}} must contain at least {{min}} item."},
	{"foundry.min_length", "{{attribute}} must contain at least {{min}} characters.", "min", "number", "{{attribute}} must contain at least {{min}} character."},
	{"foundry.multiple_of", "{{attribute}} must be a multiple of {{divisor}}.", "divisor", "text", ""},
	{"foundry.non_blank", "{{attribute}} must not be blank.", "", "", ""},
	{"foundry.non_empty", "{{attribute}} must not be empty.", "", "", ""},
	{"foundry.not_matches", "{{attribute}} has a prohibited format.", "", "", ""},
	{"foundry.not_nil", "{{attribute}} must not be null.", "", "", ""},
	{"foundry.not_null", "{{attribute}} must not be null.", "", "", ""},
	{"foundry.not_one_of", "{{attribute}} is a prohibited value.", "", "", ""},
	{"foundry.one_of", "{{attribute}} is not an allowed value.", "", "", ""},
	{"foundry.password", "{{attribute}} does not meet the password requirements.", "", "", ""},
	{"foundry.present", "{{attribute}} must be supplied.", "", "", ""},
	{"foundry.prohibited", "{{attribute}} must be omitted or empty.", "", "", ""},
	{"foundry.required", "{{attribute}} must be supplied and must not be empty.", "", "", ""},
	{"foundry.same", "{{attribute}} must match {{other}}.", "other", "text", ""},
	{"foundry.starts_with", "{{attribute}} has an invalid prefix.", "", "", ""},
	{"foundry.time", "{{attribute}} must be a time.", "", "", ""},
	{"foundry.timezone", "{{attribute}} must be a valid timezone.", "", "", ""},
	{"foundry.ulid", "{{attribute}} must be a ULID.", "", "", ""},
	{"foundry.unique", "{{attribute}} is already in use.", "", "", ""},
	{"foundry.uppercase", "{{attribute}} must be uppercase.", "", "", ""},
	{"foundry.url", "{{attribute}} must be an absolute HTTP or HTTPS URL.", "", "", ""},
	{"foundry.uuid", "{{attribute}} must be a UUID.", "", "", ""},
}

func (m builtinMessage) definition() i18n.MessageDefinition {
	d := i18n.MessageDefinition{Key: i18n.MessageKey("validation." + strings.TrimPrefix(string(m.id), "foundry.")), Parameters: []i18n.Parameter{{Name: "attribute", Kind: i18n.TextParameter}}}
	if m.argument != "" {
		d.Parameters = append(d.Parameters, i18n.Parameter{Name: m.argument, Kind: m.kind})
	}
	if m.one != "" {
		d.Plural = m.argument
		d.Kind = i18n.Cardinal
	}
	return d
}

// MessageDefinitions registers every built-in rule's translation signature.
// The returned values are owned snapshots. English fallback text is retained
// by the rule even when English is not an enabled catalog/request locale.
func MessageDefinitions() []i18n.MessageDefinition {
	result := make([]i18n.MessageDefinition, 0, len(builtinMessages))
	for _, m := range builtinMessages {
		result = append(result, m.definition())
	}
	return result
}

func builtinPrepared(spec Spec) (i18n.PreparedMessage, error) {
	for _, m := range builtinMessages {
		if m.id != spec.ID {
			continue
		}
		args := map[string]i18n.Argument{"attribute": i18n.Text("This field")}
		if m.argument != "" {
			text := ""
			name := m.argument
			if name == "other" {
				name = "value"
				text = "the related field"
			}
			for _, p := range spec.Parameters {
				if p.Name != name {
					continue
				}
				text = string(p.Value)
				if len(text) > 0 && text[0] == '"' {
					if err := json.Unmarshal(p.Value, &text); err != nil {
						return i18n.PreparedMessage{}, err
					}
				}
			}
			if m.kind == i18n.NumberParameter {
				number, err := decimal.Parse(text)
				if err != nil {
					return i18n.PreparedMessage{}, err
				}
				args[m.argument] = i18n.Number(number)
			} else {
				args[m.argument] = i18n.Text(text)
			}
		}
		fallback := i18n.Template{Text: m.text}
		if m.one != "" {
			fallback = i18n.Template{Forms: map[i18n.PluralForm]string{i18n.One: m.one, i18n.Other: m.text}}
		}
		return i18n.PrepareMessage(m.definition(), args, fallback)
	}
	return i18n.PreparedMessage{}, invalid("unknown built-in validation message")
}
