// Package openadr is a subset of OpenADR 3.0 (the JSON/REST profile of the demand-response
// standard): a Virtual Top Node (VTN) that a grid operator publishes programs and events
// to, and a Virtual End Node (VEN) client that polls the VTN and turns the active event
// into a signal for the market engine.
//
// Implemented: programs and events (create, list, get, update, delete), bearer-token
// access, the payload types SIMPLE (level 0-3), PRICE and DISPATCH_SETPOINT, targets by
// group, interval periods in ISO 8601 durations, and event priority.
//
// Not implemented: reports, subscriptions/webhooks, VEN registration, OAuth token
// issuance, and the full set of payload types. This is NOT an OpenADR Alliance certified
// implementation; it follows the 3.0 resource shapes so a standard client can be pointed
// at it for the parts above.
package openadr

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// Payload types we understand.
const (
	PayloadSimple   = "SIMPLE"            // demand-response level 0 (normal) to 3 (severe)
	PayloadPrice    = "PRICE"             // $/kWh price signal
	PayloadDispatch = "DISPATCH_SETPOINT" // kW the aggregator is asked to deliver
)

// Program is a demand-response program run by a grid operator.
type Program struct {
	ID          string `json:"id,omitempty"`
	ProgramName string `json:"programName"`
	CreatedAt   string `json:"createdDateTime,omitempty"`
}

// Target scopes an event, for example {"type":"GROUP","values":["Z1","Z2"]}.
type Target struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
}

// IntervalPeriod is when an interval starts and how long it lasts (ISO 8601 duration, e.g. PT15M).
type IntervalPeriod struct {
	Start    string `json:"start"`
	Duration string `json:"duration"`
}

// Payload is one signal value inside an interval.
type Payload struct {
	Type   string    `json:"type"`
	Values []float64 `json:"values"`
}

// Interval carries payloads. Without its own IntervalPeriod it follows the event's period,
// each interval starting where the previous one ended.
type Interval struct {
	ID             int             `json:"id"`
	IntervalPeriod *IntervalPeriod `json:"intervalPeriod,omitempty"`
	Payloads       []Payload       `json:"payloads"`
}

// Event is a signal to the fleet.
type Event struct {
	ID             string          `json:"id,omitempty"`
	ProgramID      string          `json:"programID"`
	EventName      string          `json:"eventName,omitempty"`
	Priority       int             `json:"priority"` // 0 is the highest priority
	Targets        []Target        `json:"targets,omitempty"`
	IntervalPeriod *IntervalPeriod `json:"intervalPeriod,omitempty"`
	Intervals      []Interval      `json:"intervals"`
	CreatedAt      string          `json:"createdDateTime,omitempty"`
}

var durRe = regexp.MustCompile(`^P(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$`)

// ParseDuration reads the ISO 8601 durations OpenADR uses: P1D, PT15M, PT1H30M, PT45S.
func ParseDuration(s string) (time.Duration, error) {
	m := durRe.FindStringSubmatch(s)
	if m == nil || s == "P" || s == "PT" {
		return 0, fmt.Errorf("invalid ISO 8601 duration %q", s)
	}
	n := func(i int) time.Duration {
		v, _ := strconv.Atoi(m[i])
		return time.Duration(v)
	}
	return n(1)*24*time.Hour + n(2)*time.Hour + n(3)*time.Minute + n(4)*time.Second, nil
}

func parsePeriod(p *IntervalPeriod) (time.Time, time.Duration, error) {
	if p == nil {
		return time.Time{}, 0, errors.New("missing intervalPeriod")
	}
	start, err := time.Parse(time.RFC3339, p.Start)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("bad start %q: %w", p.Start, err)
	}
	d, err := ParseDuration(p.Duration)
	if err != nil {
		return time.Time{}, 0, err
	}
	if d <= 0 {
		return time.Time{}, 0, errors.New("duration must be positive")
	}
	return start, d, nil
}

// Validate checks an event is well formed.
func (e Event) Validate() error {
	if e.ProgramID == "" {
		return errors.New("programID is required")
	}
	if e.Priority < 0 {
		return errors.New("priority must be >= 0")
	}
	if len(e.Intervals) == 0 {
		return errors.New("at least one interval is required")
	}
	for i, iv := range e.Intervals {
		p := iv.IntervalPeriod
		if p == nil {
			p = e.IntervalPeriod
		}
		if _, _, err := parsePeriod(p); err != nil {
			return fmt.Errorf("interval %d: %w", i, err)
		}
		if len(iv.Payloads) == 0 {
			return fmt.Errorf("interval %d has no payloads", i)
		}
		for _, pl := range iv.Payloads {
			switch pl.Type {
			case PayloadSimple, PayloadPrice, PayloadDispatch:
			default:
				return fmt.Errorf("interval %d: unsupported payload type %q", i, pl.Type)
			}
			if len(pl.Values) == 0 {
				return fmt.Errorf("interval %d: payload %s has no values", i, pl.Type)
			}
			if pl.Type == PayloadSimple && (pl.Values[0] < 0 || pl.Values[0] > 3) {
				return fmt.Errorf("interval %d: SIMPLE level must be 0-3", i)
			}
			if pl.Type == PayloadPrice && pl.Values[0] < 0 {
				return fmt.Errorf("interval %d: PRICE must not be negative", i)
			}
		}
	}
	return nil
}

// window returns when interval i is in force.
func (e Event) window(i int) (time.Time, time.Time, error) {
	iv := e.Intervals[i]
	if iv.IntervalPeriod != nil {
		s, d, err := parsePeriod(iv.IntervalPeriod)
		return s, s.Add(d), err
	}
	s, d, err := parsePeriod(e.IntervalPeriod)
	if err != nil {
		return s, s, err
	}
	start := s.Add(time.Duration(i) * d)
	return start, start.Add(d), nil
}

// Signal is what the active events add up to for one VEN at one moment.
type Signal struct {
	Level    int     // SIMPLE level 0-3
	Price    float64 // PRICE, $/kWh (0 = none)
	Dispatch float64 // DISPATCH_SETPOINT, kW (0 = none)
	EventID  string  // the event that supplied the level (empty when none is active)
}

func targets(e Event, groups []string) bool {
	if len(e.Targets) == 0 {
		return true // untargeted events apply to everyone
	}
	for _, t := range e.Targets {
		if t.Type != "GROUP" {
			continue
		}
		for _, v := range t.Values {
			for _, g := range groups {
				if v == g {
					return true
				}
			}
		}
	}
	return false
}

// ActiveSignal combines the events in force at `now` for a VEN in the given groups. When
// several events set the same kind of signal, the one with the highest priority (lowest
// number) wins; ties go to the most recently created.
func ActiveSignal(events []Event, now time.Time, groups []string) Signal {
	type pick struct {
		prio    int
		created string
		set     bool
	}
	var level, price, disp pick
	var sig Signal
	better := func(p *pick, e Event) bool {
		return !p.set || e.Priority < p.prio || (e.Priority == p.prio && e.CreatedAt > p.created)
	}
	for _, e := range events {
		if !targets(e, groups) {
			continue
		}
		for i := range e.Intervals {
			start, end, err := e.window(i)
			if err != nil || now.Before(start) || !now.Before(end) {
				continue
			}
			for _, pl := range e.Intervals[i].Payloads {
				v := pl.Values[0]
				switch pl.Type {
				case PayloadSimple:
					if better(&level, e) {
						level, sig.Level, sig.EventID = pick{e.Priority, e.CreatedAt, true}, int(v), e.ID
					}
				case PayloadPrice:
					if better(&price, e) {
						price, sig.Price = pick{e.Priority, e.CreatedAt, true}, v
					}
				case PayloadDispatch:
					if better(&disp, e) {
						disp, sig.Dispatch = pick{e.Priority, e.CreatedAt, true}, v
					}
				}
			}
		}
	}
	return sig
}
