package model

import "time"

// TargetRequest describes an outbound request recorded inside a Welog request log.
type TargetRequest struct {
	URL         string
	Method      string
	ContentType string
	Header      map[string]interface{}
	Body        []byte
	Timestamp   time.Time
}
