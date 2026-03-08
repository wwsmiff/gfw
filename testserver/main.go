package main

import (
	"fmt"
	"net/http"
)

func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "hello")
}

func main() {
	http.HandleFunc("/health", healthHandler)

	fmt.Println("Server running on :8080")
	http.ListenAndServe(":8080", nil)
}
