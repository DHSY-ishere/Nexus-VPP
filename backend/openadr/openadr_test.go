package openadr

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

var noon = time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

func ev(id string, prio int, start time.Time, dur string, payloads ...Payload) Event {
	return Event{ID: id, ProgramID: "p", Priority: prio, CreatedAt: id,
		IntervalPeriod: &IntervalPeriod{Start: start.Format(time.RFC3339), Duration: dur},
		Intervals:      []Interval{{ID: 0, Payloads: payloads}}}
}

func simple(l float64) Payload { return Payload{Type: PayloadSimple, Values: []float64{l}} }
func price(p float64) Payload  { return Payload{Type: PayloadPrice, Values: []float64{p}} }

func TestParseDuration(t *testing.T) {
	ok := map[string]time.Duration{"PT15M": 15 * time.Minute, "PT1H30M": 90 * time.Minute, "PT45S": 45 * time.Second,
		"P1D": 24 * time.Hour, "P1DT2H": 26 * time.Hour}
	for in, want := range ok {
		if got, err := ParseDuration(in); err != nil || got != want {
			t.Errorf("%s = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "15M", "P", "PT", "PTM", "1H", "PT-5M", "garbage"} {
		if _, err := ParseDuration(in); err == nil {
			t.Errorf("%q should be rejected", in)
		}
	}
}

func TestValidate(t *testing.T) {
	good := ev("e", 0, noon, "PT15M", simple(2))
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(e *Event){
		"no program":      func(e *Event) { e.ProgramID = "" },
		"negative prio":   func(e *Event) { e.Priority = -1 },
		"no intervals":    func(e *Event) { e.Intervals = nil },
		"no period":       func(e *Event) { e.IntervalPeriod = nil },
		"bad start":       func(e *Event) { e.IntervalPeriod.Start = "yesterday" },
		"bad duration":    func(e *Event) { e.IntervalPeriod.Duration = "15 minutes" },
		"zero duration":   func(e *Event) { e.IntervalPeriod.Duration = "PT0S" },
		"no payloads":     func(e *Event) { e.Intervals[0].Payloads = nil },
		"unknown payload": func(e *Event) { e.Intervals[0].Payloads = []Payload{{Type: "MAGIC", Values: []float64{1}}} },
		"no values":       func(e *Event) { e.Intervals[0].Payloads = []Payload{{Type: PayloadPrice}} },
		"level too high":  func(e *Event) { e.Intervals[0].Payloads = []Payload{simple(4)} },
		"negative price":  func(e *Event) { e.Intervals[0].Payloads = []Payload{price(-0.1)} },
	}
	for name, mut := range cases {
		e := ev("e", 0, noon, "PT15M", simple(2))
		mut(&e)
		if e.Validate() == nil {
			t.Errorf("%s: expected a validation error", name)
		}
	}
}

func TestActiveSignalWindowsAndTargets(t *testing.T) {
	e := ev("e1", 0, noon, "PT15M", simple(3), price(0.6))
	if s := ActiveSignal([]Event{e}, noon.Add(-time.Second), nil); s != (Signal{}) {
		t.Fatalf("before start: %+v", s)
	}
	if s := ActiveSignal([]Event{e}, noon, nil); s.Level != 3 || s.Price != 0.6 || s.EventID != "e1" {
		t.Fatalf("at start: %+v", s)
	}
	if s := ActiveSignal([]Event{e}, noon.Add(15*time.Minute), nil); s != (Signal{}) {
		t.Fatalf("end is exclusive: %+v", s)
	}

	e.Targets = []Target{{Type: "GROUP", Values: []string{"Z1"}}}
	if s := ActiveSignal([]Event{e}, noon, []string{"Z2"}); s.Level != 0 {
		t.Fatalf("event for Z1 must not reach Z2: %+v", s)
	}
	if s := ActiveSignal([]Event{e}, noon, []string{"Z2", "Z1"}); s.Level != 3 {
		t.Fatalf("event for Z1 must reach a Z1 member: %+v", s)
	}
}

func TestActiveSignalPriorityAndSequentialIntervals(t *testing.T) {
	low := ev("a", 5, noon, "PT1H", simple(1))
	high := ev("b", 0, noon, "PT1H", simple(3))
	if s := ActiveSignal([]Event{low, high}, noon.Add(time.Minute), nil); s.Level != 3 || s.EventID != "b" {
		t.Fatalf("priority 0 must win: %+v", s)
	}
	tie1, tie2 := ev("2026-1", 1, noon, "PT1H", simple(1)), ev("2026-2", 1, noon, "PT1H", simple(2))
	if s := ActiveSignal([]Event{tie1, tie2}, noon, nil); s.Level != 2 {
		t.Fatalf("on a tie the newest wins: %+v", s)
	}

	// Intervals without their own period follow one another from the event start.
	seq := Event{ID: "s", ProgramID: "p", CreatedAt: "s", IntervalPeriod: &IntervalPeriod{Start: noon.Format(time.RFC3339), Duration: "PT30M"},
		Intervals: []Interval{{ID: 0, Payloads: []Payload{simple(1)}}, {ID: 1, Payloads: []Payload{simple(3)}}}}
	if s := ActiveSignal([]Event{seq}, noon.Add(10*time.Minute), nil); s.Level != 1 {
		t.Fatalf("first interval: %+v", s)
	}
	if s := ActiveSignal([]Event{seq}, noon.Add(40*time.Minute), nil); s.Level != 3 {
		t.Fatalf("second interval: %+v", s)
	}
	if s := ActiveSignal([]Event{seq}, noon.Add(70*time.Minute), nil); s.Level != 0 {
		t.Fatalf("after the last interval: %+v", s)
	}
}

// ---- VTN over HTTP ----

func serve(t *testing.T, token string) (*VTN, *httptest.Server) {
	v := NewVTN(token)
	mux := http.NewServeMux()
	v.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return v, srv
}

func do(t *testing.T, method, url, token string, body any) (int, []byte) {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, url, rd)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.Bytes()
}

func TestVTNLifecycleAndValidation(t *testing.T) {
	_, srv := serve(t, "")
	base := srv.URL + BasePath

	if code, _ := do(t, "POST", base+"/programs", "", Program{}); code != 400 {
		t.Fatalf("program without a name: %d", code)
	}
	code, body := do(t, "POST", base+"/programs", "", Program{ProgramName: "Grid emergency"})
	var prog Program
	json.Unmarshal(body, &prog)
	if code != 201 || prog.ID == "" {
		t.Fatalf("create program: %d %s", code, body)
	}

	e := ev("", 0, noon, "PT15M", simple(2))
	e.ProgramID = "prog-nope"
	if code, _ := do(t, "POST", base+"/events", "", e); code != 400 {
		t.Fatalf("event with unknown program: %d", code)
	}
	e.ProgramID = prog.ID
	e.Intervals[0].Payloads = []Payload{{Type: "MAGIC", Values: []float64{1}}}
	if code, _ := do(t, "POST", base+"/events", "", e); code != 400 {
		t.Fatalf("event with bad payload: %d", code)
	}

	e.Intervals[0].Payloads = []Payload{simple(2)}
	code, body = do(t, "POST", base+"/events", "", e)
	var created Event
	json.Unmarshal(body, &created)
	if code != 201 || created.ID == "" || created.CreatedAt == "" {
		t.Fatalf("create event: %d %s", code, body)
	}
	if code, _ := do(t, "GET", base+"/events/"+created.ID, "", nil); code != 200 {
		t.Fatalf("get: %d", code)
	}
	if code, _ := do(t, "GET", base+"/events/evt-9999", "", nil); code != 404 {
		t.Fatalf("get missing: %d", code)
	}

	created.Intervals[0].Payloads = []Payload{simple(3)}
	code, body = do(t, "PUT", base+"/events/"+created.ID, "", created)
	var updated Event
	json.Unmarshal(body, &updated)
	if code != 200 || updated.Intervals[0].Payloads[0].Values[0] != 3 || updated.CreatedAt != created.CreatedAt {
		t.Fatalf("update: %d %s", code, body)
	}

	_, body = do(t, "GET", base+"/events?programID="+prog.ID, "", nil)
	var list []Event
	json.Unmarshal(body, &list)
	if len(list) != 1 {
		t.Fatalf("list: %s", body)
	}
	if _, body = do(t, "GET", base+"/events?programID=other", "", nil); string(bytes.TrimSpace(body)) != "[]" {
		t.Fatalf("filtered list should be an empty array, got %s", body)
	}
	if code, _ := do(t, "DELETE", base+"/events/"+created.ID, "", nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	if code, _ := do(t, "GET", base+"/events/"+created.ID, "", nil); code != 404 {
		t.Fatalf("deleted event still found: %d", code)
	}
}

func TestVTNRequiresBearerToken(t *testing.T) {
	_, srv := serve(t, "s3cret")
	url := srv.URL + BasePath + "/events"
	if code, _ := do(t, "GET", url, "", nil); code != 401 {
		t.Fatalf("no token: %d", code)
	}
	if code, _ := do(t, "GET", url, "wrong", nil); code != 401 {
		t.Fatalf("wrong token: %d", code)
	}
	if code, _ := do(t, "GET", url, "s3cret", nil); code != 200 {
		t.Fatalf("right token: %d", code)
	}
}

// ---- VEN ----

func TestVENAppliesChangesAndKeepsSignalWhenVTNIsDown(t *testing.T) {
	vtn, srv := serve(t, "tok")
	base := srv.URL + BasePath
	_, body := do(t, "POST", base+"/programs", "tok", Program{ProgramName: "DR"})
	var prog Program
	json.Unmarshal(body, &prog)

	clock := noon
	v := NewVEN(srv.URL, "tok", []string{"Z1"})
	v.Now = func() time.Time { return clock }

	var mu sync.Mutex
	var seen []Signal
	var errs int
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		v.Run(ctx, 20*time.Millisecond,
			func(s Signal) { mu.Lock(); seen = append(seen, s); mu.Unlock() },
			func(error) { mu.Lock(); errs++; mu.Unlock() })
		close(done)
	}()
	waitFor := func(cond func() bool, what string) {
		t.Helper()
		for i := 0; i < 200; i++ {
			mu.Lock()
			ok := cond()
			mu.Unlock()
			if ok {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s (seen %+v)", what, seen)
	}

	waitFor(func() bool { return len(seen) == 1 && seen[0] == Signal{} }, "initial empty signal")

	e := ev("", 0, noon, "PT1H", simple(3), price(0.7))
	e.ProgramID = prog.ID
	e.Targets = []Target{{Type: "GROUP", Values: []string{"Z1"}}}
	do(t, "POST", base+"/events", "tok", e)
	waitFor(func() bool { return len(seen) == 2 && seen[1].Level == 3 && seen[1].Price == 0.7 }, "event signal")

	n := len(seen)
	time.Sleep(100 * time.Millisecond) // many polls, no change: apply must not fire again
	mu.Lock()
	if len(seen) != n {
		t.Fatalf("apply fired %d extra times for an unchanged signal", len(seen)-n)
	}
	mu.Unlock()

	srv.Close() // the VTN disappears
	waitFor(func() bool { return errs > 0 }, "poll errors")
	mu.Lock()
	if len(seen) != n {
		t.Fatalf("signal changed during an outage: %+v", seen)
	}
	mu.Unlock()

	cancel()
	<-done
	_ = vtn
}
