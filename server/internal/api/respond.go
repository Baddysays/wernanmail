package api

import (
	"encoding/json"
	"net/http"
)

// Error codes — UI translates; never return localized human strings.
const (
	CodeOK               = "ok"
	CodeInvalidRequest   = "mail.invalid_request"
	CodeAuthFailed       = "mail.auth_failed"
	CodeSessionRequired  = "mail.session_required"
	CodeSessionInvalid   = "mail.session_invalid"
	CodeFolderListFailed = "mail.folder_list_failed"
	CodeFetchFailed      = "mail.fetch_failed"
	CodeMessageNotFound  = "mail.message_not_found"
	CodeSendFailed       = "mail.send_failed"
	CodeFlagFailed       = "mail.flag_failed"
	CodeTrashFailed      = "mail.trash_failed"
	CodeInternal         = "mail.internal_error"
)

type errorBody struct {
	Code string `json:"code"`
}

type dataBody struct {
	Data any `json:"data"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, errorBody{Code: code})
}

func writeData(w http.ResponseWriter, status int, data any) {
	writeJSON(w, status, dataBody{Data: data})
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}
