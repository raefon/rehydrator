package db

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestLegacySourceKindMigration(t *testing.T) {
	dsn := os.Getenv("REHYDRATOR_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	ctx := context.Background()
	repo, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	schema := pgx.Identifier{"legacy_source_" + time.Now().Format("150405000000000")}.Sanitize()
	if _, err := tx.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL search_path TO "+schema); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `CREATE TABLE media_cache_state(id UUID PRIMARY KEY,infohash TEXT,magnet TEXT);
 CREATE TABLE media_cache_torrent_sources(media_id UUID PRIMARY KEY,infohash TEXT NOT NULL,filename TEXT NOT NULL DEFAULT '',torrent_file BYTEA NOT NULL);
 CREATE TABLE media_cache_grab_sources(id INTEGER PRIMARY KEY,ready BOOLEAN,magnet TEXT,torrent_file BYTEA,torrent_url TEXT,filename TEXT,last_error TEXT);
 INSERT INTO media_cache_state VALUES
 ('00000000-0000-0000-0000-000000000001','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'),
 ('00000000-0000-0000-0000-000000000002','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb','magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb&tr=udp://tracker'),
 ('00000000-0000-0000-0000-000000000003','cccccccccccccccccccccccccccccccccccccccc',NULL),
 ('00000000-0000-0000-0000-000000000004','dddddddddddddddddddddddddddddddddddddddd','magnet:?xt=urn:btih:dddddddddddddddddddddddddddddddddddddddd');
 INSERT INTO media_cache_torrent_sources VALUES ('00000000-0000-0000-0000-000000000004','dddddddddddddddddddddddddddddddddddddddd','',decode('6465','hex'));
 INSERT INTO media_cache_grab_sources VALUES
 (1,true,'magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',NULL,'','',NULL),
 (2,true,'magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb&tr=udp://tracker',NULL,'','',NULL),
 (3,true,'old-generated-magnet',decode('6465','hex'),'','',NULL);`)
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../migrations/022_explicit_torrent_source_kinds.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"recovery_required", "magnet", "recovery_required", "torrent_file"} {
		var kind string
		id := "00000000-0000-0000-0000-00000000000" + string(rune('1'+i))
		if err := tx.QueryRow(ctx, `SELECT source_kind FROM media_cache_torrent_sources WHERE media_id=$1`, id).Scan(&kind); err != nil || kind != want {
			t.Fatalf("row %d kind=%s want=%s err=%v", i, kind, want, err)
		}
	}
	var ready bool
	var kind, magnet, filename string
	if err := tx.QueryRow(ctx, `SELECT ready,source_kind FROM media_cache_grab_sources WHERE id=1`).Scan(&ready, &kind); err != nil || ready || kind != "recovery_required" {
		t.Fatal("ambiguous grab not marked for recovery", err)
	}
	if err := tx.QueryRow(ctx, `SELECT source_kind,magnet,filename FROM media_cache_grab_sources WHERE id=3`).Scan(&kind, &magnet, &filename); err != nil || kind != "torrent_file" || magnet != "" || filename == "" {
		t.Fatal("file grab not made exclusive", err)
	}
	// A newly captured real bare magnet has explicit provenance and must survive
	// repeated automatic schema initialization without being reclassified.
	if _, err := tx.Exec(ctx, `UPDATE media_cache_torrent_sources SET source_kind='magnet',magnet='magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' WHERE media_id='00000000-0000-0000-0000-000000000001'`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(migration)); err != nil {
		t.Fatal("migration not repeatable", err)
	}
	if err := tx.QueryRow(ctx, `SELECT source_kind FROM media_cache_torrent_sources WHERE media_id='00000000-0000-0000-0000-000000000001'`).Scan(&kind); err != nil || kind != "magnet" {
		t.Fatal("explicit bare magnet was reclassified", err)
	}
	if _, err := tx.Exec(ctx, "SAVEPOINT invalid_source"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE media_cache_torrent_sources SET torrent_file=decode('6465','hex') WHERE source_kind='magnet'`); err == nil || !strings.Contains(err.Error(), "ck_saved_source_payload") {
		t.Fatal("database accepted mixed sources", err)
	}
	if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT invalid_source"); err != nil {
		t.Fatal(err)
	}
}
