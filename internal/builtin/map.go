package builtin

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/datasplice-labs/datasplice-core/internal/contract"
	"github.com/datasplice-labs/datasplice-core/internal/record"
)

// Map is a transform builtin driven entirely by its "with:" block.
type Map struct {
	selectKeys []string
	rename     map[string]string
	types      map[string]string
}

func NewMap() *Map { return &Map{} }

func (m *Map) Describe() contract.Describe {
	return contract.Describe{Name: "map", Version: "0.1.0", Role: contract.RoleTransform}
}

func (m *Map) Configure(settings map[string]any, fn string, on []string, secrets map[string]string) error {
	if fn != "" {
		return fmt.Errorf("map: does not declare any functions, got fn=%q", fn)
	}

	if sel, ok := settings["select"].([]any); ok {
		for _, s := range sel {
			if str, ok := s.(string); ok {
				m.selectKeys = append(m.selectKeys, str)
			}
		}
	}

	if ren, ok := settings["rename"].(map[string]any); ok {
		m.rename = map[string]string{}
		for k, v := range ren {
			if str, ok := v.(string); ok {
				m.rename[k] = str
			}
		}
	}

	if typ, ok := settings["types"].(map[string]any); ok {
		m.types = map[string]string{}
		for k, v := range typ {
			if str, ok := v.(string); ok {
				m.types[k] = str
			}
		}
	}
	return nil
}

func (m *Map) Process(ctx context.Context, in <-chan contract.Batch, out chan<- contract.Batch) error {
	for {
		select {
		case batch, ok := <-in:
			if !ok {
				return nil
			}
			result := make(contract.Batch, 0, len(batch))
			for _, rec := range batch {
				r2, err := m.apply(rec)
				if err != nil {
					return fmt.Errorf("map: %w", err)
				}
				result = append(result, r2)
			}
			select {
			case out <- result:
			case <-ctx.Done():
				return ctx.Err()
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (m *Map) apply(rec record.Record) (record.Record, error) {
	out := record.Record{}
	keys := m.selectKeys
	if len(keys) == 0 {
		for k := range rec {
			keys = append(keys, k)
		}
	}
	for _, k := range keys {
		v, ok := rec.Get(k)
		if !ok {
			continue
		}
		name := k
		if renamed, ok := m.rename[k]; ok {
			name = renamed
		}
		if t, ok := m.types[k]; ok {
			converted, err := convert(v, t)
			if err != nil {
				return nil, fmt.Errorf("column %q: %w", k, err)
			}
			v = converted
		}
		out.Set(name, v)
	}
	return out, nil
}

func convert(v any, typ string) (any, error) {
	s := fmt.Sprint(v)
	switch typ {
	case "string":
		return s, nil
	case "number":
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot convert %q to number", s)
		}
		return f, nil
	case "bool":
		return s == "true" || s == "1", nil
	case "timestamp":
		// RFC3339 only. Add other layouts when a real source needs one.
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return nil, fmt.Errorf("cannot parse %q as timestamp (want RFC3339)", s)
		}
		return t.Format(time.RFC3339), nil
	default:
		return nil, fmt.Errorf("unknown type %q", typ)
	}
}
