package db

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestLegacyStateConstraintRepair(t *testing.T) {
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
	schema := pgx.Identifier{"state_repair_" + time.Now().Format("150405000000000")}.Sanitize()
	if _, err := tx.Exec(ctx, "CREATE SCHEMA "+schema+"; SET LOCAL search_path TO "+schema); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, schemaSQL); err != nil {
		t.Fatal(err)
	}
	legacy := `ALTER TABLE media_cache_state DROP CONSTRAINT media_cache_state_state_check;
 ALTER TABLE media_cache_state ADD CONSTRAINT media_cache_state_state_check CHECK (state IN ('REQUESTED','REARMING'));`
	migration, err := os.ReadFile("../../migrations/024_repair_lifecycle_state_constraint.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, repair := range []string{schemaSQL, string(migration)} {
		if _, err := tx.Exec(ctx, `DELETE FROM media_cache_state;`+legacy); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if _, err := tx.Exec(ctx, repair); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO media_cache_state(tenant,media_type,arr_id,symlink_path,state) VALUES('test','movie',83,'/movie.mkv','REARMING'); UPDATE media_cache_state SET state='WAITING_FOR_VISIBILITY';`); err != nil {
			t.Fatal(err)
		}
	}
}
