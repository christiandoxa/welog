package model

import "time"

// TargetResponse describes an outbound response recorded inside a Welog request log.
type TargetResponse struct {
	Header  map[string]interface{}
	Body    []byte
	Status  int
	Latency time.Duration
}
