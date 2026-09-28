package temporal

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
)

// Service binds immutable calendar helpers to one application clock and zone.
// Retain it in domain constructors. Instants and their JSON remain UTC; local
// parsing, presentation and calendar arithmetic use this service's timezone.
type Service struct {
	source clock.Clock
	name   ZoneName
	zone   *time.Location
}

func NewService(source clock.Clock, name ZoneName) (Service, error) {
	if err := validateClock(source); err != nil {
		return Service{}, err
	}
	zone, err := name.Location()
	if err != nil {
		return Service{}, err
	}
	return Service{source: source, name: name, zone: zone}, nil
}

func (s Service) TimeZone() ZoneName { return s.name }

// Location returns an owned standard Go location for APIs needing explicit zones.
func (s Service) Location() *time.Location {
	if s.zone == nil {
		return nil
	}
	owned := *s.zone
	return &owned
}

// In returns another service borrowing the same clock with an explicit override.
func (s Service) In(name ZoneName) (Service, error) { return NewService(s.source, name) }
func (s Service) Now() (DateTime, error)            { return Now(s.source) }
func (s Service) Today() (Date, error)              { return Today(s.source, s.zone) }
func (s Service) Parse(text string) (DateTime, error) {
	return ParseDateTimeIn(text, s.zone)
}
func (s Service) Format(instant DateTime) (string, error) { return instant.FormatIn(s.zone) }
func (s Service) Date(instant DateTime) (Date, error)     { return instant.DateIn(s.zone) }
func (s Service) Local(instant DateTime) (LocalDateTime, error) {
	return instant.LocalIn(s.zone)
}

// StartOfDay resolves local midnight with the existing strict DST policy.
// Historical midnight gaps/overlaps return errors instead of guessing an instant.
func (s Service) StartOfDay(instant DateTime) (DateTime, error) {
	date, err := s.Date(instant)
	if err != nil {
		return DateTime{}, err
	}
	local, err := NewLocalDateTime(date, Time{})
	if err != nil {
		return DateTime{}, err
	}
	return local.In(s.zone)
}

// AddDays preserves local wall time across calendar days, which need not be 24
// elapsed hours. A resulting DST gap/overlap remains an explicit error.
func (s Service) AddDays(instant DateTime, days int) (DateTime, error) {
	local, err := s.Local(instant)
	if err != nil {
		return DateTime{}, err
	}
	date, err := local.Date().AddDays(days)
	if err != nil {
		return DateTime{}, err
	}
	shifted, err := NewLocalDateTime(date, local.Time())
	if err != nil {
		return DateTime{}, err
	}
	return shifted.In(s.zone)
}
