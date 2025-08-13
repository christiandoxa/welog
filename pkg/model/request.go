package model

import "time"

type TargetRequest struct {
	URL         string
	Method      string
	ContentType string
	Header      map[string]interface{}
	Body        []byte
	Timestamp   time.Time
}
