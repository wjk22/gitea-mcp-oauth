package user

import (
	gitea_sdk "gitea.dev/sdk"
)

func slimOrg(o *gitea_sdk.Organization) map[string]any {
	if o == nil {
		return nil
	}
	return map[string]any{
		"id":          o.ID,
		"name":        o.Name,
		"full_name":   o.FullName,
		"description": o.Description,
		"avatar_url":  o.AvatarURL,
		"website":     o.Website,
	}
}

func slimOrgs(orgs []*gitea_sdk.Organization) []map[string]any {
	out := make([]map[string]any, 0, len(orgs))
	for _, o := range orgs {
		out = append(out, slimOrg(o))
	}
	return out
}
