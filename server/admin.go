package server

import (
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"time"
)

// adminErrors are the banners /admin shows for ?error=. Only these fixed texts
// are shown, so a crafted link cannot put its own words on the admin page.
var adminErrors = map[string]string{
	"invalid_email": "Enter an email address.",
	"last_admin":    "There must always be at least one admin.",
	"internal":      "Something went wrong. Try again.",
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
	URL string
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

	online := map[string]bool{}
	webSockets := 0
	var tunnels []tunnelRow
	for _, t := range s.connManager.Snapshot() {
		online[t.Email] = true
		webSockets += t.WebSockets
		tunnels = append(tunnels, tunnelRow{t, s.forwardedProto(r) + "://" + t.Domain + "." + r.Host})
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
		"WebSockets":   webSockets,
		"Requests":     s.requests.Load(),
		"TunnelErrors": s.tunnelErrors.Load(),
		"Uptime":       time.Since(s.startedAt),
	})
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
