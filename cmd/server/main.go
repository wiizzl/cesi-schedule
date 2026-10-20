package main

import (
	"log"
	"net/http"
	"os"

	api "cesi-schedule"
)

func main() {
	addr := ":3000"
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}

	http.HandleFunc("/", api.HandleICS)

	log.Printf("Listening on http://localhost%s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
