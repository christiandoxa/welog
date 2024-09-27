package util

import "github.com/valyala/fasthttp"

// HeaderToMap converts fasthttp headers to map
func HeaderToMap(header interface{}) map[string]interface{} {
	headersMap := make(map[string]interface{})

	// check if header is *fasthttp.ResponseHeader or *fasthttp.RequestHeader

	switch header.(type) {

	case *fasthttp.ResponseHeader:
		header.(*fasthttp.ResponseHeader).VisitAll(func(key, value []byte) {
			headersMap[string(key)] = string(value)
		})

	case *fasthttp.RequestHeader:
		header.(*fasthttp.RequestHeader).VisitAll(func(key, value []byte) {
			headersMap[string(key)] = string(value)
		})

	}

	return headersMap
}
