package server

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dbackowski/wormhole/common"
)

// adminErrors are the banners /admin shows for ?error=. Only these fixed texts
// are shown, so a crafted link cannot put its own words on the admin page.
var adminErrors = map[string]string{
	"invalid_email":  "Enter an email address.",
	"last_admin":     "There must always be at least one admin.",
	"internal":       "Something went wrong. Try again.",
	"invalid_domain": "Enter a domain name: lowercase letters, digits and hyphens, up to 63.",
	"unknown_user":   "Reserve domains only for users on the list.",
	"domain_taken":   "That domain is already reserved. Release it first to give it to someone else.",
}

func redirectAdminError(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/admin?error="+code, http.StatusSeeOther)
}

type userRow struct {
	User
	Online bool
}

type tunnelRow struct {
	TunnelInfo
	URL      string
	Reserved bool
}

type domainRow struct {
	Domain
	InUseBy string // email of whoever has a tunnel open on it
}

// handleAdmin renders the dashboard straight from memory: the tunnels and
// counters are never stored, so they start over on each restart.
func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	me, ok := s.sessionAdmin(r)
	if !ok {
		if s.setupPending() {
			renderMessage(w, http.StatusOK, "No admin yet. Open the setup link from the server logs.")
			return
		}
		http.Redirect(w, r, "/admin/login", http.StatusFound)
		return
	}

	users, err := s.auth.store.Users()
	if err != nil {
		s.Logger.Error("Listing users failed", "error", err)
		renderMessage(w, http.StatusInternalServerError, "Something went wrong. Try again.")
		return
	}

	domains, err := s.auth.store.Domains()
	if err != nil {
		s.Logger.Error("Listing domains failed", "error", err)
		renderMessage(w, http.StatusInternalServerError, "Something went wrong. Try again.")
		return
	}
	reserved := map[string]bool{}
	for _, d := range domains {
		reserved[d.Name] = true
	}

	online := map[string]bool{}
	inUseBy := map[string]string{}
	webSockets := 0
	var tunnels []tunnelRow
	for _, t := range s.connManager.Snapshot() {
		online[t.Email] = true
		inUseBy[t.Domain] = t.Email
		webSockets += t.WebSockets
		tunnels = append(tunnels, tunnelRow{t, s.forwardedProto(r) + "://" + t.Domain + "." + r.Host, reserved[t.Domain]})
	}

	domainRows := make([]domainRow, len(domains))
	for i, d := range domains {
		domainRows[i] = domainRow{d, inUseBy[d.Name]}
	}

	admins := 0
	rows := make([]userRow, len(users))
	for i, u := range users {
		rows[i] = userRow{u, online[u.Email]}
		if u.IsAdmin {
			admins++
		}
	}

	render(w, http.StatusOK, "admin", map[string]any{
		"Me":           me,
		"Host":         r.Host,
		"Error":        adminErrors[r.URL.Query().Get("error")],
		"Users":        rows,
		"Admins":       admins,
		"Tunnels":      tunnels,
		"Domains":      domainRows,
		"WebSockets":   webSockets,
		"Requests":     s.requests.Load(),
		"TunnelErrors": s.tunnelErrors.Load(),
		"Uptime":       time.Since(s.startedAt),
	})
}

// handleAdminDomains reserves a domain for a user or releases it.
func (s *Server) handleAdminDomains(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.checkAdminPost(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)

	name := strings.ToLower(strings.TrimSpace(r.PostFormValue("domain")))
	action := r.PostFormValue("action")
	if !common.DomainLabel.MatchString(name) {
		redirectAdminError(w, r, "invalid_domain")
		return
	}

	switch action {
	case "reserve":
		email := normalizeEmail(r.PostFormValue("email"))
		if _, ok, err := s.auth.store.User(email); err != nil || !ok {
			redirectAdminError(w, r, "unknown_user")
			return
		}
		err := s.auth.store.ReserveDomain(name, email)
		if errors.Is(err, errDomainReserved) {
			redirectAdminError(w, r, "domain_taken")
			return
		}
		if err != nil {
			s.Logger.Error("Reserving domain failed", "domain", name, "email", email, "error", err)
			redirectAdminError(w, r, "internal")
			return
		}
		s.Logger.Info("Domain reserved", "by", admin, "domain", name, "email", email)
		// Someone else may be using it right now. Their reconnect then gets a 403.
		if s.connManager.CloseUnlessOwnedBy(name, email) {
			s.Logger.Info("Closed tunnel on newly reserved domain", "domain", name)
		}
	case "release":
		if err := s.auth.store.ReleaseDomain(name); err != nil {
			s.Logger.Error("Releasing domain failed", "domain", name, "error", err)
			redirectAdminError(w, r, "internal")
			return
		}
		s.Logger.Info("Domain released", "by", admin, "domain", name)
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

var templateFuncs = template.FuncMap{
	"since": func(t time.Time) string { return shortDuration(time.Since(t)) },
	"dur":   shortDuration,
	"num":   thousands,
}

// shortDuration is "45s", "14m", "2h 14m" or "3d 4h".
func shortDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
	}
}

// thousands formats n as "12,480".
func thousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0 && s[i-1] != '-'; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
