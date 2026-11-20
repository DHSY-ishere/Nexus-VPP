package openadr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// VEN polls a VTN and reports the signal in force.
type VEN struct {
	BaseURL string // e.g. http://127.0.0.1:8080 (BasePath is appended)
	Token   string
	Groups  []string // the groups this VEN belongs to, matched against event targets
	HTTP    *http.Client
	Now     func() time.Time
}

func NewVEN(baseURL, token string, groups []string) *VEN {
	return &VEN{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, Groups: groups,
		HTTP: &http.Client{Timeout: 5 * time.Second}, Now: time.Now}
}

// Fetch downloads the VTN's events.
func (v *VEN) Fetch(ctx context.Context) ([]Event, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.BaseURL+BasePath+"/events", nil)
	if err != nil {
		return nil, err
	}
	if v.Token != "" {
		req.Header.Set("Authorization", "Bearer "+v.Token)
	}
	resp, err := v.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("VTN returned %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var evs []Event
	if err := json.NewDecoder(resp.Body).Decode(&evs); err != nil {
		return nil, err
	}
	return evs, nil
}

// Current fetches the events and returns the signal in force now.
func (v *VEN) Current(ctx context.Context) (Signal, error) {
	evs, err := v.Fetch(ctx)
	if err != nil {
		return Signal{}, err
	}
	return ActiveSignal(evs, v.Now(), v.Groups), nil
}

// Run polls every interval and calls apply when the signal changes, until ctx ends. A failed
// poll keeps the last signal in force (a VEN must not drop a demand-response event because
// the VTN is briefly unreachable); onErr, if set, is told about it.
func (v *VEN) Run(ctx context.Context, every time.Duration, apply func(Signal), onErr func(error)) {
	var last *Signal
	tick := func() {
		s, err := v.Current(ctx)
		if err != nil {
			if onErr != nil {
				onErr(err)
			}
			return
		}
		if last == nil || *last != s {
			apply(s)
			last = &s
		}
	}
	tick()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick()
		}
	}
}
