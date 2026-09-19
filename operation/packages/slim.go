package packages

func slimPackage(v any) map[string]any {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := map[string]any{
		"id":         m["id"],
		"type":       m["type"],
		"name":       m["name"],
		"version":    m["version"],
		"html_url":   m["html_url"],
		"created_at": m["created_at"],
	}
	if owner, ok := m["owner"].(map[string]any); ok {
		out["owner"] = owner["login"]
	}
	if creator, ok := m["creator"].(map[string]any); ok {
		out["creator"] = creator["login"]
	}
	if repo, ok := m["repository"].(map[string]any); ok {
		out["repository"] = repo["full_name"]
	}
	return out
}

func slimPackages(v any) any {
	switch val := v.(type) {
	case []any:
		out := make([]map[string]any, 0, len(val))
		for _, item := range val {
			if slim := slimPackage(item); slim != nil {
				out = append(out, slim)
			}
		}
		return out
	case map[string]any:
		return slimPackage(val)
	default:
		return v
	}
}
