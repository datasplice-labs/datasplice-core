package manifest

import (
	"fmt"
	"slices"
	"time"
)

var validMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}
var validPer = []string{"second", "minute", "hour"}
var validBackoff = []string{"exponential", "linear", "fixed"}

// validateHTTP checks the parts of a manifest the HTTP engine relies on:
// connection, auth shape, rate limit, retry, and each action's request
// and pagination settings.
func (m *Manifest) validateHTTP() error {
	if m.BaseURL == "" {
		return fmt.Errorf("base_url is required")
	}

	if err := m.Auth.validate(); err != nil {
		return err
	}

	if m.RateLimit != nil {
		if err := m.RateLimit.validate("rate_limit"); err != nil {
			return err
		}

		for name, o := range m.RateLimit.Overrides {
			if _, ok := m.Actions[name]; !ok {
				return fmt.Errorf("rate_limit.overrides.%s: no such action", name)
			}

			if err := o.validate("rate_limit.overrides." + name); err != nil {
				return err
			}
		}
	}

	if err := m.Retry.validate(); err != nil {
		return err
	}

	for name, a := range m.Actions {
		if err := a.validateHTTP(name); err != nil {
			return err
		}
	}

	return nil
}

// validate checks that the fields each auth type needs are present.
func (a *Auth) validate() error {
	if a == nil {
		return nil
	}

	missing := ""
	switch {
	case a.Type == "basic" && a.Username == "":
		missing = "username"
	case a.Type == "basic" && a.Password == "":
		missing = "password"
	case a.Type == "bearer" && a.Token == "":
		missing = "token"
	case (a.Type == "header" || a.Type == "query") && a.Name == "":
		missing = "name"
	case (a.Type == "header" || a.Type == "query") && a.Value == "":
		missing = "value"
	}

	if missing != "" {
		return fmt.Errorf("auth.%s is required for type %s", missing, a.Type)
	}

	return nil
}

func (r *RateLimit) validate(field string) error {
	if r.Requests <= 0 {
		return fmt.Errorf("%s.requests must be greater than 0", field)
	}

	if !slices.Contains(validPer, r.Per) {
		return fmt.Errorf("%s.per %q is invalid (want second, minute, or hour)", field, r.Per)
	}

	return nil
}

func (r *Retry) validate() error {
	if r == nil {
		return nil
	}

	for _, code := range r.On {
		if code < 100 || code > 599 {
			return fmt.Errorf("retry.on: %d is not an HTTP status code", code)
		}
	}

	if r.MaxAttempts < 0 {
		return fmt.Errorf("retry.max_attempts must not be negative")
	}

	if r.Backoff != "" && !slices.Contains(validBackoff, r.Backoff) {
		return fmt.Errorf("retry.backoff %q is invalid (want exponential, linear, or fixed)", r.Backoff)
	}

	if r.InitialDelay != "" {
		if d, err := time.ParseDuration(r.InitialDelay); err != nil || d <= 0 {
			return fmt.Errorf("retry.initial_delay %q must be a positive duration like 1s", r.InitialDelay)
		}
	}

	return nil
}

func (a Action) validateHTTP(name string) error {
	if !slices.Contains(validMethods, a.Method) {
		return fmt.Errorf("actions.%s: method %q is invalid (want %v)", name, a.Method, validMethods)
	}

	if a.Path == "" {
		return fmt.Errorf("actions.%s: path is required", name)
	}

	if a.Paginate == nil {
		return nil
	}

	if a.Role != "source" {
		return fmt.Errorf("actions.%s: paginate is only valid on source actions", name)
	}

	return a.Paginate.validate(name)
}

func (p *Paginate) validate(action string) error {
	switch p.Style {
	case "cursor":
		if p.Next == "" {
			return fmt.Errorf("actions.%s.paginate: next is required for style cursor", action)
		}
	case "page":
		if p.Param == "" {
			return fmt.Errorf("actions.%s.paginate: param is required for style page", action)
		}
	case "offset", "link_header":
		return fmt.Errorf("actions.%s.paginate: style %q is not allowed (want cursor or page)", action, p.Style)
	default:
		return fmt.Errorf("actions.%s.paginate: style %q is invalid (want cursor or page)", action, p.Style)
	}

	if _, err := ParseUntil(p.Until); err != nil {
		return fmt.Errorf("actions.%s.paginate: %w", action, err)
	}

	return nil
}
