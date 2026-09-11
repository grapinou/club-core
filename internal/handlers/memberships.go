package handlers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/grapinou/club-core/internal/accounts"
	"github.com/grapinou/club-core/internal/auth"
	"github.com/grapinou/club-core/internal/authorization"
	"github.com/grapinou/club-core/internal/database/dbsqlc"
	"github.com/grapinou/club-core/internal/memberships"
	"github.com/grapinou/club-core/internal/views"
	"github.com/grapinou/club-core/internal/websecurity"
	"github.com/jackc/pgx/v5"
)

type MembershipReader interface {
	List(context.Context) ([]memberships.ListEntry, error)
	GetDetails(context.Context, int32) (memberships.Details, error)
}
type MembershipActions interface {
	ApproveMembership(context.Context, int32, int32, *string) (accounts.ApprovalResult, error)
	ResendActivation(context.Context, int32, int32) (accounts.ResendResult, error)
}
type MembershipAssociationReader interface {
	GetMembershipIDForUser(context.Context, dbsqlc.GetMembershipIDForUserParams) (int32, error)
}
type MembershipHandler struct {
	siteName    string
	location    *time.Location
	reader      MembershipReader
	actions     MembershipActions
	association MembershipAssociationReader
	permissions PermissionChecker
}

func NewMembershipHandler(site string, loc *time.Location, reader MembershipReader, actions MembershipActions, q MembershipAssociationReader, permissions PermissionChecker) *MembershipHandler {
	return &MembershipHandler{siteName: site, location: loc, reader: reader, actions: actions, association: q, permissions: permissions}
}
func (h *MembershipHandler) Register(mux *http.ServeMux, access *Access, csrf *websecurity.CSRF) {
	mux.Handle("GET /memberships", access.RequirePermission(authorization.MembershipsRead, csrf.Protect(http.HandlerFunc(h.list))))
	mux.Handle("GET /memberships/{id}", access.RequirePermission(authorization.MembershipsRead, csrf.Protect(http.HandlerFunc(h.detail))))
	mux.Handle("POST /memberships/{id}/approve", access.RequirePermission(authorization.MembershipsApprove, csrf.Protect(http.HandlerFunc(h.approve))))
	mux.Handle("POST /users/{id}/resend-activation", access.RequirePermission(authorization.ActivationResend, csrf.Protect(http.HandlerFunc(h.resend))))
}
func membershipID(w http.ResponseWriter, r *http.Request) (int32, bool) {
	id, err := parseID(r.PathValue("id"))
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return 0, false
	}
	return id, true
}
func membershipError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, authorization.ErrForbidden) {
		http.Error(w, "Accès refusé", 403)
		return
	}
	http.Error(w, "Erreur interne du serveur", 500)
}
func writeMembershipPage(w http.ResponseWriter, buf *bytes.Buffer, err error) {
	if err != nil {
		http.Error(w, "Erreur interne du serveur", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}
func (h *MembershipHandler) list(w http.ResponseWriter, r *http.Request) {
	rows, err := h.reader.List(r.Context())
	if err != nil {
		membershipError(w, r, err)
		return
	}
	v := views.MembershipListView{SecurityData: pageSecurity(r), SiteName: h.siteName, Title: "Adhésions - " + h.siteName, Rows: views.MembershipRows(rows, h.location)}
	var buf bytes.Buffer
	err = views.RenderMembershipList(&buf, v)
	writeMembershipPage(w, &buf, err)
}
func (h *MembershipHandler) detail(w http.ResponseWriter, r *http.Request) {
	id, ok := membershipID(w, r)
	if !ok {
		return
	}
	d, err := h.reader.GetDetails(r.Context(), id)
	if err != nil {
		membershipError(w, r, err)
		return
	}
	actor, _ := auth.UserID(r.Context())
	approve, err := h.permissions.HasPermission(r.Context(), actor, authorization.MembershipsApprove)
	if err != nil {
		membershipError(w, r, err)
		return
	}
	resend, err := h.permissions.HasPermission(r.Context(), actor, authorization.ActivationResend)
	if err != nil {
		membershipError(w, r, err)
		return
	}
	v := views.MembershipDetail(d, h.location, approve, resend)
	v.SecurityData = pageSecurity(r)
	v.SiteName = h.siteName
	v.Title = "Dossier d'adhésion - " + h.siteName
	if notice, ok := membershipNotices[r.URL.Query().Get("notice")]; ok {
		v.Notice = notice.text
		v.NoticeClass = notice.class
	}
	var buf bytes.Buffer
	err = views.RenderMembershipDetail(&buf, v)
	writeMembershipPage(w, &buf, err)
}

type membershipNotice struct{ text, class string }

// Only fixed, non-personal result markers appear in PRG URLs. The current dossier
// state, not the marker, determines available actions and completeness.
var membershipNotices = map[string]membershipNotice{
	"approved":             {"Adhésion validée.", "alert-success"},
	"approved_sent":        {"Adhésion validée et email d'activation envoyé.", "alert-success"},
	"approved_no_channel":  {"Adhésion validée. Aucun email d'activation n'est disponible.", "alert-warning"},
	"approved_send_failed": {"Adhésion validée, mais email d'activation non envoyé.", "alert-warning"},
	"incomplete":           {"Le dossier ne peut pas être validé. Consultez les blocages ci-dessous.", "alert-danger"},
	"already_processed":    {"Cette adhésion a déjà été traitée et ne peut pas être approuvée à nouveau.", "alert-info"},
	"resent":               {"Email d'activation renvoyé.", "alert-success"},
	"resend_no_channel":    {"Nouveau code préparé, mais aucun email n'est disponible.", "alert-warning"},
	"resend_send_failed":   {"Nouveau code préparé, mais email non envoyé.", "alert-warning"},
	"resend_unavailable":   {"Le compte ne permet pas un renvoi d'activation.", "alert-info"},
}

func redirectMembership(w http.ResponseWriter, r *http.Request, id int32, notice string) {
	http.Redirect(w, r, fmt.Sprintf("/memberships/%d?notice=%s", id, notice), http.StatusSeeOther)
}
func (h *MembershipHandler) approve(w http.ResponseWriter, r *http.Request) {
	id, ok := membershipID(w, r)
	if !ok {
		return
	}
	actor, ok := auth.UserID(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", 303)
		return
	}
	var note *string
	if text := strings.TrimSpace(r.PostForm.Get("admin_note")); text != "" {
		note = &text
	}
	result, err := h.actions.ApproveMembership(r.Context(), id, actor, note)
	var delivery *accounts.DeliveryError
	if errors.As(err, &delivery) && delivery.ApprovalCommitted {
		redirectMembership(w, r, id, "approved_send_failed")
		return
	}
	var incomplete *memberships.IncompleteError
	if errors.As(err, &incomplete) {
		redirectMembership(w, r, id, "incomplete")
		return
	}
	if errors.Is(err, memberships.ErrNotPending) {
		redirectMembership(w, r, id, "already_processed")
		return
	}
	if err != nil {
		membershipError(w, r, err)
		return
	}
	notice := "approved"
	switch result.DeliveryStatus {
	case accounts.Sent:
		notice = "approved_sent"
	case accounts.NoChannel:
		notice = "approved_no_channel"
	}
	redirectMembership(w, r, id, notice)
}
func (h *MembershipHandler) resend(w http.ResponseWriter, r *http.Request) {
	userID, ok := membershipID(w, r)
	if !ok {
		return
	}
	actor, ok := auth.UserID(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", 303)
		return
	}
	membership, err := strconv.ParseInt(r.PostForm.Get("membership_id"), 10, 32)
	if err != nil || membership <= 0 {
		http.NotFound(w, r)
		return
	}
	// Validate this association before using the supplied dossier as a return path.
	id, err := h.association.GetMembershipIDForUser(r.Context(), dbsqlc.GetMembershipIDForUserParams{MembershipID: int32(membership), UserID: userID})
	if err != nil {
		membershipError(w, r, err)
		return
	}
	result, err := h.actions.ResendActivation(r.Context(), actor, userID)
	var delivery *accounts.DeliveryError
	if errors.As(err, &delivery) {
		redirectMembership(w, r, id, "resend_send_failed")
		return
	}
	if errors.Is(err, accounts.ErrAlreadyActivated) || errors.Is(err, accounts.ErrDisabled) {
		redirectMembership(w, r, id, "resend_unavailable")
		return
	}
	if err != nil {
		membershipError(w, r, err)
		return
	}
	notice := "resent"
	if result.DeliveryStatus == accounts.NoChannel {
		notice = "resend_no_channel"
	}
	redirectMembership(w, r, id, notice)
}
