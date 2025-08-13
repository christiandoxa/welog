package model

import "time"

type TargetResponse struct {
	Header  map[string]interface{}
	Body    []byte
	Status  int
	Latency time.Duration
}
