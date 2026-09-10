// Package redact keeps secret values out of every output path: plan
// output, run logs, error formatting, and --dry-run samples
// (datasplice-core-prd.md §2 "Redaction"). Route output through a Writer
// (or String, for error messages built before printing) rather than
// remembering to redact at each call site — the failure mode being
// defended against is the one place someone forgot.
package redact

import (
	"io"
	"strings"
)

type Writer struct {
	w       io.Writer
	secrets []string
}

// New builds a Writer that replaces every occurrence of a value in
// secretValues with "***". Empty values are ignored so an unset secret
// can't accidentally redact everything.
func New(w io.Writer, secretValues map[string]string) *Writer {
	values := make([]string, 0, len(secretValues))
	for _, v := range secretValues {
		if v != "" {
			values = append(values, v)
		}
	}
	return &Writer{w: w, secrets: values}
}

func (r *Writer) Write(p []byte) (int, error) {
	if _, err := io.WriteString(r.w, r.String(string(p))); err != nil {
		return 0, err
	}
	return len(p), nil
}

// String redacts a string directly, for error messages assembled before
// they're printed.
func (r *Writer) String(s string) string {
	for _, secret := range r.secrets {
		s = strings.ReplaceAll(s, secret, "***")
	}
	return s
}
