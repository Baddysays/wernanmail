package api

import (
	"fmt"
	"log"
	"mime"
	"net/http"
	"strconv"
	"strings"

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
	var offset uint32
	if raw := r.URL.Query().Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > 100_000 {
			writeError(w, http.StatusBadRequest, CodeInvalidRequest)
			return
		}
		offset = uint32(n)
	}

	msgs, err := mail.ListMessages(sess.Creds, folder, limit, offset)
	if err != nil {
		writeError(w, http.StatusBadGateway, CodeFetchFailed)
		return
	}
	writeData(w, http.StatusOK, msgs)
}

func (h *Handler) SearchMessages(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	folder := r.URL.Query().Get("folder")
	if folder == "" {
		folder = "INBOX"
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	var limit uint32 = 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			writeError(w, http.StatusBadRequest, CodeInvalidRequest)
			return
		}
		limit = uint32(n)
	}
	msgs, err := mail.SearchMessages(sess.Creds, folder, q, limit)
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

func (h *Handler) GetAttachment(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	id := chi.URLParam(r, "id")
	part := chi.URLParam(r, "part")
	folder := r.URL.Query().Get("folder")
	if folder == "" {
		folder = "INBOX"
	}
	if part == "" {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest)
		return
	}

	filename, contentType, data, err := mail.GetAttachment(sess.Creds, folder, id, part)
	if err != nil {
		if mail.IsNotFound(err) {
			writeError(w, http.StatusNotFound, CodeMessageNotFound)
			return
		}
		writeError(w, http.StatusBadGateway, CodeFetchFailed)
		return
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if filename == "" {
		filename = "attachment"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	cd := mime.FormatMediaType("attachment", map[string]string{"filename": filename})
	if cd == "" {
		cd = fmt.Sprintf("attachment; filename=%q", filename)
	}
	w.Header().Set("Content-Disposition", cd)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (h *Handler) SendMessage(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	r.Body = http.MaxBytesReader(w, r.Body, 32<<20) // ~32 MiB JSON (base64 overhead)
	var req mail.SendRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest)
		return
	}
	if len(req.To) == 0 {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest)
		return
	}
	if _, err := mail.DecodeOutboundAttachments(req.Attachments); err != nil {
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
		if strings.Contains(err.Error(), "attachment") || strings.Contains(err.Error(), "size limit") {
			writeError(w, http.StatusBadRequest, CodeInvalidRequest)
			return
		}
		writeError(w, http.StatusBadGateway, CodeSendFailed)
		return
	}
	log.Printf("send ok user=%s smtp=%s:%d recipients=%d",
		sess.Creds.Username, sess.Creds.SMTPHost, sess.Creds.SMTPPort, len(req.To))
	writeData(w, http.StatusOK, map[string]string{"status": "sent"})
}

func (h *Handler) SaveDraft(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
	var req mail.SendRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest)
		return
	}
	if _, err := mail.DecodeOutboundAttachments(req.Attachments); err != nil {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest)
		return
	}
	if err := mail.SaveDraft(sess.Creds, req); err != nil {
		log.Printf("draft failed user=%s: %v", sess.Creds.Username, err)
		writeError(w, http.StatusBadGateway, CodeSendFailed)
		return
	}
	writeData(w, http.StatusOK, map[string]string{"status": "draft"})
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
	result, err := mail.TrashMessage(sess.Creds, folder, id)
	if err != nil {
		writeError(w, http.StatusBadGateway, CodeTrashFailed)
		return
	}
	writeData(w, http.StatusOK, result)
}

func (h *Handler) MoveMessage(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	id := chi.URLParam(r, "id")
	var body struct {
		Folder   string `json:"folder"`
		ToFolder string `json:"toFolder"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest)
		return
	}
	if body.Folder == "" {
		body.Folder = "INBOX"
	}
	if body.ToFolder == "" {
		writeError(w, http.StatusBadRequest, CodeInvalidRequest)
		return
	}
	if err := mail.MoveMessage(sess.Creds, body.Folder, body.ToFolder, id); err != nil {
		writeError(w, http.StatusBadGateway, CodeTrashFailed)
		return
	}
	writeData(w, http.StatusOK, map[string]string{"status": "moved"})
}
