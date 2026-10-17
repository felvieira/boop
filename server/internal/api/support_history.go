package api

import "net/http"

type supportCardAuditEntry struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	Field     string `json:"field"`
	OldValue  string `json:"old_value"`
	NewValue  string `json:"new_value"`
	Actor     string `json:"actor"`
	CreatedAt string `json:"created_at"`
}

func (s *Server) supportCardHistory(w http.ResponseWriter, r *http.Request) {
	cardID := r.PathValue("id")
	var exists int
	if err := s.DB.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM support_cards WHERE id=?)`, cardID).Scan(&exists); err != nil {
		s.fail(w, err)
		return
	}
	if exists == 0 {
		writeError(w, http.StatusNotFound, "support_card_not_found", "the support card does not exist")
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `SELECT id,action,field,old_value,new_value,actor,created_at
		FROM support_card_audit WHERE card_id=? ORDER BY created_at DESC,id DESC LIMIT 100`, cardID)
	if err != nil {
		s.fail(w, err)
		return
	}
	defer rows.Close()
	history := make([]supportCardAuditEntry, 0)
	for rows.Next() {
		var entry supportCardAuditEntry
		if err := rows.Scan(&entry.ID, &entry.Action, &entry.Field, &entry.OldValue, &entry.NewValue, &entry.Actor, &entry.CreatedAt); err != nil {
			s.fail(w, err)
			return
		}
		history = append(history, entry)
	}
	if err := rows.Err(); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": history})
}
