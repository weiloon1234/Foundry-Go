package i18n

// LocaleRequirement selects which supported locales locale-keyed input, such
// as translated model text, must supply with nonempty text. The zero value
// requires none.
type LocaleRequirement uint8

const (
	OptionalLocales LocaleRequirement = iota
	DefaultLocale
	AllLocales
)

func (r LocaleRequirement) Validate() error {
	if r > AllLocales {
		return invalidLocale()
	}
	return nil
}

// Required returns the locales of one catalog snapshot that r requires, in
// the snapshot's order.
func (r LocaleRequirement) Required(locales LocaleSet) []LocaleID {
	switch r {
	case DefaultLocale:
		return []LocaleID{locales.Default()}
	case AllLocales:
		return locales.Locales()
	}
	return nil
}
