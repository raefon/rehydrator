package db

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/raefon/rehydrator/internal/uilogs"
)

func (r *Repo) PublishLogs(ctx context.Context, tenant, instance, source string, entries []uilogs.Entry) error {
	payload, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `INSERT INTO ui_log_snapshots (tenant,instance,source,entries,updated_at) VALUES ($1,$2,$3,$4,now()) ON CONFLICT (tenant,instance) DO UPDATE SET entries=EXCLUDED.entries,updated_at=now()`, tenant, instance, source, payload)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `DELETE FROM ui_log_snapshots WHERE tenant=$1 AND updated_at < now()-interval '10 minutes'`, tenant)
	return err
}

func (r *Repo) RecentUILogs(ctx context.Context, tenant string) ([]uilogs.Entry, []string, error) {
	rows, err := r.pool.Query(ctx, `SELECT source,entries FROM ui_log_snapshots WHERE tenant=$1 AND updated_at > now()-interval '10 seconds' ORDER BY instance LIMIT 20`, tenant)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	entries := []uilogs.Entry{}
	sources := []string{}
	seen := map[string]bool{}
	for rows.Next() {
		var source string
		var payload []byte
		if err := rows.Scan(&source, &payload); err != nil {
			return nil, nil, err
		}
		var batch []uilogs.Entry
		if err := json.Unmarshal(payload, &batch); err != nil {
			return nil, nil, err
		}
		entries = append(entries, batch...)
		if !seen[source] {
			sources = append(sources, source)
			seen[source] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	sort.SliceStable(entries, func(i, j int) bool {
		a, _ := time.Parse(time.RFC3339Nano, entries[i].Time)
		b, _ := time.Parse(time.RFC3339Nano, entries[j].Time)
		return a.Before(b)
	})
	if len(entries) > 600 {
		entries = entries[len(entries)-600:]
	}
	sort.Strings(sources)
	return entries, sources, nil
}
