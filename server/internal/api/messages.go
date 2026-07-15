package api

import (
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/Baddysays/wernanmail/server/internal/mail"
	"github.com/Baddysays/wernanmail/server/internal/mailtmpl"
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
	var policy mailtmpl.Policy
	if h.OutboundPolicy != nil {
		policy = h.OutboundPolicy()
	}
	if err := mail.SendMessageWithPolicy(sess.Creds, req, policy); err != nil {
		log.Printf("send failed user=%s smtp=%s:%d tls=%v: %v",
			sess.Creds.Username, sess.Creds.SMTPHost, sess.Creds.SMTPPort, sess.Creds.TLS, err)
		writeError(w, http.StatusBadGateway, CodeSendFailed)
		return
	}
	writeData(w, http.StatusOK, map[string]string{"status": "sent"})
}

func (h *Handler) UpdateMessageFlags(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	id := chi.URLParam(r, "id")
	var body struct {
		Folder string   `json:"folder"`
		Add    []string `json:"add"`
		Remove []string `json:"remove"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest)
		return
	}
	if body.Folder == "" {
		body.Folder = "INBOX"
	}
	if err := mail.UpdateFlags(sess.Creds, body.Folder, id, mail.FlagUpdate{
		Add:    body.Add,
		Remove: body.Remove,
	}); err != nil {
		writeError(w, http.StatusBadGateway, CodeFlagFailed)
		return
	}
	writeData(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) TrashMessage(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	id := chi.URLParam(r, "id")
	folder := r.URL.Query().Get("folder")
	if folder == "" {
		var body struct {
			Folder string `json:"folder"`
		}
		_ = decodeJSON(r, &body)
		folder = body.Folder
	}
	if folder == "" {
		folder = "INBOX"
	}
	if err := mail.TrashMessage(sess.Creds, folder, id); err != nil {
		writeError(w, http.StatusBadGateway, CodeTrashFailed)
		return
	}
	writeData(w, http.StatusOK, map[string]string{"status": "trashed"})
}
