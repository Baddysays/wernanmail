package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/Baddysays/wernanmail/server/internal/mail"
)

func (h *Handler) ListMessages(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	folder := r.URL.Query().Get("folder")
	if folder == "" {
		folder = "INBOX"
	}
	var limit uint32 = 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			writeError(w, http.StatusBadRequest, CodeInvalidRequest)
			return
		}
		limit = uint32(n)
	}

	msgs, err := mail.ListMessages(sess.Creds, folder, limit)
	if err != nil {
		writeError(w, http.StatusBadGateway, CodeFetchFailed)
		return
	}
	writeData(w, http.StatusOK, msgs)
}

func (h *Handler) GetMessage(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	id := chi.URLParam(r, "id")
	folder := r.URL.Query().Get("folder")
	if folder == "" {
		folder = "INBOX"
	}

	msg, err := mail.GetMessage(sess.Creds, folder, id)
	if err != nil {
		if mail.IsNotFound(err) {
			writeError(w, http.StatusNotFound, CodeMessageNotFound)
			return
		}
		writeError(w, http.StatusBadGateway, CodeFetchFailed)
		return
	}
	writeData(w, http.StatusOK, msg)
}

func (h *Handler) SendMessage(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	var req mail.SendRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest)
		return
	}
	if len(req.To) == 0 {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest)
		return
	}
	if err := mail.SendMessage(sess.Creds, req); err != nil {
		writeError(w, http.StatusBadGateway, CodeSendFailed)
		return
	}
	writeData(w, http.StatusOK, map[string]string{"status": "sent"})
}
