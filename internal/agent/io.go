package agent

import (
	"bufio"
	"encoding/json"
	"io"
)

// ReadMsg decodes one wire message from r, skipping blank lines. It returns
// io.EOF when r is exhausted so a caller can loop until the other side closes
// the pipe.
func ReadMsg(r io.Reader) (*Msg, error) {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadBytes('\n')
		trimmed := line
		if len(trimmed) > 0 && trimmed[len(trimmed)-1] == '\n' {
			trimmed = trimmed[:len(trimmed)-1] // strip trailing newline
		}
		if len(trimmed) == 0 {
			// Blank line (or empty read). Tolerate a final line without a
			// trailing newline by treating EOF-with-data as data, not error.
			if err == nil || err != io.EOF {
				continue
			}
			return nil, io.EOF
		}
		m, perr := ReadMsgLine(trimmed)
		if perr != nil {
			return nil, perr
		}
		return m, nil
	}
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
