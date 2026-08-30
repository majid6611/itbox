package api

import "net/http"

// handleWebSocket sits outside huma (see router.go) since it needs the raw
// http.ResponseWriter/Request to hand to the websocket library's upgrade
// call. Auth still works the same way as every other endpoint — the
// browser sends the itp_employee_session cookie on the upgrade request
// same as any other request to this origin, so it's read directly here
// rather than through huma's cookie-binding input structs.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(employeeSessionCookieName)
	if err != nil {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}
	username, err := s.requireEmployeeAuth(r.Context(), cookie.Value)
	if err != nil {
		http.Error(w, "not authenticated", http.StatusUnauthorized)
		return
	}
	s.Hub.Serve(w, r, username)
}
