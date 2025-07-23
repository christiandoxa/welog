package util

import "github.com/valyala/fasthttp"

// HeaderToMap converts fasthttp headers to map
func HeaderToMap(header interface{}) map[string]interface{} {
	headersMap := make(map[string]interface{})

	// check if header is *fasthttp.ResponseHeader or *fasthttp.RequestHeader

	switch h := header.(type) {

	case *fasthttp.ResponseHeader:
		h.All()(func(key, value []byte) bool {
			headersMap[string(key)] = string(value)
			return true
		})

	case *fasthttp.RequestHeader:
		h.All()(func(key, value []byte) bool {
			headersMap[string(key)] = string(value)
			return true
		})

	}

	return headersMap
}
