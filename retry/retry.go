package retry

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"

	log "github.com/sirupsen/logrus"
)

type wrapResponseWriter struct {
	http.ResponseWriter
	buffer *bytes.Buffer
	code   int
}

func newWrapResponseWriter(w http.ResponseWriter) *wrapResponseWriter {
	return &wrapResponseWriter{
		ResponseWriter: w,
		buffer:         bytes.NewBuffer([]byte("")),
		code:           0,
	}
}

func (w *wrapResponseWriter) WriteHeader(statusCode int) {
	for k := range w.ResponseWriter.Header() {
		delete(w.ResponseWriter.Header(), k)
	}
	w.code = statusCode
}

func (w *wrapResponseWriter) Write(data []byte) (int, error) {
	log.Debugf("Write %v, buffer %v", string(data), w.buffer)
	w.buffer.Reset()
	return w.buffer.Write(data)
}

func requestBody(r *http.Request) ([]byte, error) {
	bodyBytes, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("read request error:%v", err)
	}

	r.Body.Close()
	return bodyBytes, nil
}

// TRY is the number of forwarding the request in case error.
var TRY = 3
var retryCode = map[int]bool{
	http.StatusInternalServerError: true,
	http.StatusBadGateway:          true,
	http.StatusServiceUnavailable:  true,
	http.StatusGatewayTimeout:      true,
}

func shouldRetry(code int) bool {
	return retryCode[code]
}

// Retry buffer the request and resend it in case of getting retryCode.
func Retry(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := requestBody(r)
		if err != nil {
			log.Errorf("bufferRequestBody err= %v", err)
		}
		ww := newWrapResponseWriter(w)

		var count = 1
		for {
			r.Body = ioutil.NopCloser(bytes.NewBuffer(body))
			next.ServeHTTP(ww, r)
			log.Debugf("[Retry]%dth try request, response code %d", count, ww.code)
			if !shouldRetry(ww.code) || count >= TRY {
				break
			}
			count++
			// If WriteHeader has not yet been called, Write calls
			// WriteHeader(http.StatusOK) before writing the data.
			// So set default http.StatusOK before retry
			ww.code = http.StatusOK
		}

		ww.ResponseWriter.WriteHeader(ww.code)
		io.Copy(w, ww.buffer)
	})
}
