package handler

import (
	"net/http"

	api "cesi-schedule"
)

func Handler(w http.ResponseWriter, r *http.Request) {
	api.HandleICS(w, r)
}
