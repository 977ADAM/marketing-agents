package response

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// DecodeJSON applies a byte limit and accepts exactly one JSON value.
func DecodeJSON(w http.ResponseWriter, r *http.Request, maxBytes int64, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	dec := json.NewDecoder(r.Body)
	err := dec.Decode(out)
	if err == nil {
		var extra any
		err = dec.Decode(&extra)
		if err == io.EOF {
			return true
		}
		if err == nil {
			err = errors.New("multiple JSON values")
		}
	}
	var max *http.MaxBytesError
	if errors.As(err, &max) {
		WriteError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body exceeds limit")
	} else {
		WriteError(w, http.StatusBadRequest, "bad_json", "invalid JSON body")
	}
	return false
}
