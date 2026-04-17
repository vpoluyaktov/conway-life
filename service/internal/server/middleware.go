package server

import (
	"encoding/json"
	"log"
	"net/http"
)

func setJSON(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	setJSON(w)
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		log.Printf("jsonError encode: %v", err)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	setJSON(w)
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON encode: %v", err)
	}
}
