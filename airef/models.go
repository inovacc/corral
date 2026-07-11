package airef

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
)

// modelsEndpoint describes how to list a provider's models over its REST API.
type modelsEndpoint struct {
	url     string
	headers map[string]string
	field   string // JSON array field holding the model objects
	idKey   string // object key carrying the model id
	trim    string // prefix to strip from the id (Gemini: "models/")
}

func (p Provider) modelsEndpoint(key string) modelsEndpoint {
	switch p.Name {
	case "anthropic":
		return modelsEndpoint{
			url:     "https://api.anthropic.com/v1/models",
			headers: map[string]string{"x-api-key": key, "anthropic-version": "2023-06-01"},
			field:   "data", idKey: "id",
		}
	case "gemini":
		return modelsEndpoint{
			url:   "https://generativelanguage.googleapis.com/v1beta/models?key=" + key,
			field: "models", idKey: "name", trim: "models/",
		}
	case "openrouter":
		return modelsEndpoint{
			url:     "https://openrouter.ai/api/v1/models",
			headers: map[string]string{"Authorization": "Bearer " + key},
			field:   "data", idKey: "id",
		}
	default: // openai
		return modelsEndpoint{
			url:     "https://api.openai.com/v1/models",
			headers: map[string]string{"Authorization": "Bearer " + key},
			field:   "data", idKey: "id",
		}
	}
}

// LiveModels lists the model IDs a provider currently serves, using key for auth.
// Errors are kept generic (no URL/key detail — Gemini carries the key in the URL).
func LiveModels(ctx context.Context, p Provider, key string) ([]string, error) {
	if key == "" {
		return nil, fmt.Errorf("no API key")
	}
	e := p.modelsEndpoint(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request")
	}
	for k, v := range e.headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return nil, fmt.Errorf("decode response")
	}
	var arr []map[string]json.RawMessage
	if err := json.Unmarshal(top[e.field], &arr); err != nil {
		return nil, fmt.Errorf("decode %q array", e.field)
	}
	ids := make([]string, 0, len(arr))
	for _, m := range arr {
		var id string
		if err := json.Unmarshal(m[e.idKey], &id); err != nil {
			continue
		}
		ids = append(ids, strings.TrimPrefix(id, e.trim))
	}
	sort.Strings(ids)
	return ids, nil
}

// KeyFunc resolves the API key for a provider ("" => skip that provider). It
// keeps airef decoupled from any particular key source: callers pass EnvKey,
// or their own resolver (e.g. a secrets store).
type KeyFunc func(p Provider) string

// EnvKey resolves each provider's key from its environment variable.
func EnvKey(p Provider) string { return os.Getenv(p.KeyEnv) }

// ModelListing is the per-provider result of ListModels.
type ModelListing struct {
	Provider string   `json:"provider"`
	KeyEnv   string   `json:"key_env"`
	Count    int      `json:"count"`
	Models   []string `json:"models,omitempty"`
	Status   string   `json:"status"` // "ok" | "skipped (no key)" | "error: ..."
}

// ListModels lists live models for every provider (or only==name when set)
// whose key keyFn resolves. It never fails as a whole — per-provider problems
// are recorded in each listing's Status.
func ListModels(ctx context.Context, keyFn KeyFunc, only string) []ModelListing {
	var out []ModelListing
	for _, p := range Providers() {
		if only != "" && p.Name != only {
			continue
		}
		l := ModelListing{Provider: p.Name, KeyEnv: p.KeyEnv}
		key := keyFn(p)
		switch {
		case key == "":
			l.Status = "skipped (no key)"
		default:
			ids, err := LiveModels(ctx, p, key)
			if err != nil {
				l.Status = "error: " + err.Error()
			} else {
				l.Count, l.Models, l.Status = len(ids), ids, "ok"
			}
		}
		out = append(out, l)
	}
	return out
}

// RenderModels writes a human-readable listing to w. limit 0 prints all.
func RenderModels(w io.Writer, listings []ModelListing, limit int) {
	for _, r := range listings {
		if r.Status != "ok" {
			fmt.Fprintf(w, "%s (%s): %s\n\n", r.Provider, r.KeyEnv, r.Status)
			continue
		}
		fmt.Fprintf(w, "%s (%s): %d models\n", r.Provider, r.KeyEnv, r.Count)
		show := r.Models
		if limit > 0 && len(show) > limit {
			show = show[:limit]
		}
		for _, id := range show {
			fmt.Fprintf(w, "  %s\n", id)
		}
		if limit > 0 && r.Count > limit {
			fmt.Fprintf(w, "  … (+%d more; --limit 0 for all)\n", r.Count-limit)
		}
		fmt.Fprintln(w)
	}
}
