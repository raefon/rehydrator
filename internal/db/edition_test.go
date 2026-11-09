package db

import (
	"context"
	"fmt"
	"github.com/raefon/rehydrator/internal/model"
	"os"
	"testing"
	"time"
)

func TestSameArrIDAcrossLibraryEditions(t *testing.T) {
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
	if err = repo.InitSchema(ctx); err != nil {
		t.Fatal(err)
	}
	tenant := fmt.Sprintf("edition-test-%d", time.Now().UnixNano())
	defer repo.pool.Exec(ctx, "DELETE FROM media_cache_state WHERE tenant IN ($1,$1||':4k')", tenant)
	for _, edition := range []string{"", ":4k"} {
		if _, err = repo.pool.Exec(ctx, `INSERT INTO media_cache_state(tenant,media_type,arr_id,symlink_path,state) VALUES($1,'movie',1,$2,'AVAILABLE')`, tenant+edition, "/media"+edition+"/movie.mkv"); err != nil {
			t.Fatal(err)
		}
	}
	a, found, err := repo.GetState(ctx, tenant, model.MediaMovie, 1)
	if err != nil || !found {
		t.Fatal(err)
	}
	b, found, err := repo.GetState(ctx, tenant+":4k", model.MediaMovie, 1)
	if err != nil || !found {
		t.Fatal(err)
	}
	if a.ID == b.ID || a.SymlinkPath == b.SymlinkPath {
		t.Fatal("Arr identity collision")
	}
	if _, err = repo.RequestRearm(ctx, tenant+":4k", model.MediaMovie, 1); err != nil {
		t.Fatal(err)
	}
	a, _, err = repo.GetState(ctx, tenant, model.MediaMovie, 1)
	if err != nil || a.RearmRequested {
		t.Fatal("4k recovery touched standard row", err)
	}
}
