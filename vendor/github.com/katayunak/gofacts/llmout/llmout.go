// Package llmout decodes JSON a model was asked to return. Model output is
// untrusted input: it arrives wrapped in markdown fences, followed by prose,
// with fields nobody asked for, or far larger than any honest answer.
package llmout

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// MaxBytes bounds what will be decoded. An answer this large is a runaway
// generation, not an answer.
const MaxBytes = 4 << 20

// StripFence removes one surrounding ``` or ```json fence.
func StripFence(b []byte) []byte {
	s := strings.TrimSpace(string(b))
	if !strings.HasPrefix(s, "```") {
		return []byte(s)
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	} else {
		s = strings.TrimPrefix(s, "```")
	}
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return []byte(strings.TrimSpace(s))
}

// Decode accepts unknown fields but exactly one JSON value.
func Decode(b []byte, v any) error { return decode(b, v, false) }

// DecodeStrict also rejects fields the target does not declare, which is how a
// renamed or hallucinated field is caught instead of silently dropped.
func DecodeStrict(b []byte, v any) error { return decode(b, v, true) }

var ErrTrailing = errors.New("unexpected content after the JSON value")

func decode(b []byte, v any, strict bool) error {
	if len(b) > MaxBytes {
		return fmt.Errorf("answer is %d bytes, more than the %d allowed", len(b), MaxBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(StripFence(b)))
	if strict {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("not valid JSON: %w", err)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrTrailing
	}
	return nil
}
