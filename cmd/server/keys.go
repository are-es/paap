package main

import (
	"net/http"

	"github.com/dolvin/paap/internal/db"
)

func keyList(w http.ResponseWriter, r *http.Request) {
	rows, err := db.DB.Query(`SELECT k.id, k.provider_id, k.name, k.is_active, k.last_used, k.created_at, p.name as provider_name,
		COALESCE(k.fail_count,0), COALESCE(k.last_error,'')
		FROM api_keys k LEFT JOIN providers p ON k.provider_id=p.id ORDER BY k.created_at DESC`)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	var list []map[string]interface{}
	for rows.Next() {
		var id, providerID, name, createdAt, lastError string
		var isActive, failCount int
		var lastUsed, providerName *string
		rows.Scan(&id, &providerID, &name, &isActive, &lastUsed, &createdAt, &providerName, &failCount, &lastError)
		list = append(list, map[string]interface{}{
			"id": id, "provider_id": providerID, "name": name,
			"is_active": isActive == 1, "last_used": lastUsed,
			"provider_name": providerName, "created_at": createdAt,
			"fail_count": failCount, "last_error": lastError,
		})
	}
	if list == nil {
		list = []map[string]interface{}{}
	}
	writeJSON(w, list)
}

func keyCreate(w http.ResponseWriter, r *http.Request) {
	providerKeyCreate(w, r, "")
}

func keyRoutes(w http.ResponseWriter, r *http.Request) {
	writeError(w, 501, "not implemented yet")
}
