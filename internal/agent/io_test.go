package agent

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestReadMsgLineRoundTrip(t *testing.T) {
	msgs := []*Msg{
		{Kind: Hello},
		{Kind: SessionStart, Session: &SessionSpec{Hours: 2.5, Actions: 127}},
		{Kind: Action, Tick: 330, Detail: "clicked Gnome at 5123"},
		{Kind: Stats},
		{Kind: Ping},
		{Kind: Shutdown},
	}
	for _, m := range msgs {
		var buf bytes.Buffer
		if err := WriteMsg(&buf, m); err != nil {
			t.Fatalf("WriteMsg %v: %v", m.Kind, err)
		}
		got, err := ReadMsgLine(buf.Bytes()[:len(buf.Bytes())-1])
		if err != nil {
			t.Fatalf("ReadMsgLine %v: %v", m.Kind, err)
		}
		if got.Kind != m.Kind || got.Tick != m.Tick || got.Detail != m.Detail {
			t.Errorf("%v round trip = %+v, want %+v", m.Kind, got, m)
		}
		if m.Session != nil {
			if got.Session == nil || got.Session.Hours != m.Session.Hours || got.Session.Actions != m.Session.Actions {
				t.Errorf("session spec lost in round trip: %+v", got)
			}
		}
	}
}

func TestReadMsgSkipsBlankLinesAndToleratesMissingFinalNewline(t *testing.T) {
	src := "\n" + `{"kind":"ping"}` + "\n\n" + `{"kind":"stats"}` // last line: no trailing \n
	m, err := ReadMsg(strings.NewReader(src))
	if err != nil {
		t.Fatalf("first msg: %v", err)
	}
	if m.Kind != Ping {
		t.Errorf("first msg = %v, want ping", m.Kind)
	}
	m, err = ReadMsg(strings.NewReader(`{"kind":"stats"}`))
	if err != nil {
		t.Fatalf("no-final-newline msg: %v", err)
	}
	if m.Kind != Stats {
		t.Errorf("second msg = %v, want stats", m.Kind)
	}
	if _, err := ReadMsg(strings.NewReader("")); !errors.Is(err, io.EOF) {
		t.Errorf("empty reader: got %v, want io.EOF", err)
	}
}

func TestReadMsgLineRejectsUnknownKind(t *testing.T) {
	_, err := ReadMsgLine([]byte(`{"kind":"teleport"}`))
	if !errors.Is(err, ErrUnknownKind) {
		t.Errorf("got %v, want ErrUnknownKind", err)
	}
}

func TestReadMsgLineRejectsMalformedJSON(t *testing.T) {
	_, err := ReadMsgLine([]byte(`{not json`))
	if err == nil || errors.Is(err, ErrUnknownKind) {
		t.Errorf("got %v, want a JSON unmarshal error", err)
	}
}
