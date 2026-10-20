package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"cesi-schedule/internal/schedule"
)

func main() {
	addr := ":3000"
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	h, err := schedule.NewHandler(ctx)
	cancel()
	if err != nil {
		log.Fatal(err)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      45 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	log.Printf("Listening on http://localhost%s", addr)
	log.Fatal(srv.ListenAndServe())
}
