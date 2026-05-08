package rest

import (
	"encoding/json"
	"net/http"
)

// Ok writes a 200 JSON body. Mirrors github.com/wind-c/comqtt/v2/mqtt/rest.Ok
// so handlers in this package don't need to alias-import upstream's helpers.
func Ok(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

// Error writes a JSON error body with the given status code.
func Error(w http.ResponseWriter, code int, err string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if e := json.NewEncoder(w).Encode(err); e != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}
