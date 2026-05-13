/*
 * Copyright 2026 coze-dev Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

package rag

import (
	"fmt"
	"strings"
	"time"
)

// RagTime is a time.Time that tolerates rag's naive ISO timestamps
// (e.g. "2026-05-13T23:00:40.901540") in addition to RFC3339. Rag uses
// pydantic's default datetime serialization which drops timezone info on
// naive datetimes; coze's stdlib time.UnmarshalJSON only accepts RFC3339
// and would otherwise reject the whole response.
type RagTime time.Time

// ragTimeLayouts is tried in order. RFC3339Nano comes first because that's
// what a fixed rag will emit; the trailing layouts cover today's pydantic
// default (microseconds, no tz) and the no-fractional fallback.
var ragTimeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04:05",
}

func (r *RagTime) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	for _, layout := range ragTimeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			*r = RagTime(t.UTC())
			return nil
		}
	}
	return fmt.Errorf("rag timestamp %q matched no known layout", s)
}

func (r RagTime) MarshalJSON() ([]byte, error) {
	return []byte(`"` + time.Time(r).Format(time.RFC3339Nano) + `"`), nil
}

// UnixMilli mirrors time.Time.UnixMilli so callers can keep their existing
// .UnixMilli() usage without an explicit conversion.
func (r RagTime) UnixMilli() int64 {
	return time.Time(r).UnixMilli()
}

// Time returns the underlying time.Time for the rare caller that needs
// the full stdlib API.
func (r RagTime) Time() time.Time {
	return time.Time(r)
}
