package agent

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
)

// ReadMsg decodes one wire message from r, skipping blank lines. It returns
// io.EOF when r is exhausted so a caller can loop until the other side closes
// the pipe. A final line without a trailing newline (a peer that died
// mid-write) is still returned: its bytes were real, and whether they parse
// is the caller's concern.
func ReadMsg(r io.Reader) (*Msg, error) {
	br := bufio.NewReader(r)
	var line []byte
	for {
		n, err := br.ReadBytes('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			// A real transport failure (broken pipe, etc.): surface it —
			// whatever partial bytes arrived die with the connection.
			return nil, err
		}
		line = append(line, n...)
		if len(line) == 0 {
			if errors.Is(err, io.EOF) {
				return nil, io.EOF
			}
			continue // empty read, no error: keep going
		}
		if line[len(line)-1] != '\n' {
			break // final line without a trailing newline (EOF-with-data)
		}
		trimmed := line[:len(line)-1] // strip trailing newline
		if len(trimmed) == 0 {
			continue // blank line: skip
		}
		return ReadMsgLine(trimmed)
	}
	if len(line) == 0 || line[len(line)-1] == '\n' {
		// The final read ended on a newline (or nothing arrived): the peer's
		// last line was complete, so there is no partial message to salvage.
		return nil, io.EOF
	}
	return ReadMsgLine(line)
}

// ReadMsgLine decodes a single wire message from raw line bytes. It is split
// out from ReadMsg so tests can pin the exact error for each malformed shape
// without driving a reader.
func ReadMsgLine(line []byte) (*Msg, error) {
	var m Msg
	if err := json.Unmarshal(line, &m); err != nil {
		return nil, err
	}
	switch m.Kind {
	case Hello, SessionStart, SessionEnd, Action, Stats, Shutdown, Ping, Pong:
	default:
		return nil, ErrUnknownKind
	}
	return &m, nil
}

// WriteMsg encodes m as one line and writes it to w.
func WriteMsg(w io.Writer, m *Msg) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}
