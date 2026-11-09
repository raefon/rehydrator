package syncer

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/raefon/rehydrator/internal/db"
	"github.com/raefon/rehydrator/internal/seerr"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestSeerrPollingSeparatesEditions(t *testing.T) {
	dsn := os.Getenv("REHYDRATOR_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("requires disposable PostgreSQL")
	}
	ctx := context.Background()
	repo, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	if err = repo.InitSchema(ctx); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	tenant := fmt.Sprintf("edition-poll-%d", time.Now().UnixNano())
	defer conn.Exec(ctx, "DELETE FROM media_cache_seerr_requests WHERE tenant IN ($1,$1||':4k')", tenant)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"id":42,"is4k":false,"status":1,"media":{"mediaType":"tv","tmdbId":100}},{"id":43,"is4k":true,"status":1,"media":{"mediaType":"tv","tmdbId":100}}]}`))
	}))
	defer server.Close()
	for _, fourk := range []bool{false, true} {
		scope := tenant
		want := "request:42"
		if fourk {
			scope += ":4k"
			want = "request:43"
		}
		sync := NewSeerr(SeerrOptions{Repo: repo, Seerr: seerr.NewClient(server.URL, "key"), Tenant: scope, Is4K: fourk})
		if err = sync.SyncOnce(ctx); err != nil {
			t.Fatal(err)
		}
		rows, err := conn.Query(ctx, "SELECT request_key FROM media_cache_seerr_requests WHERE tenant=$1", scope)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for rows.Next() {
			var key string
			if err = rows.Scan(&key); err != nil {
				t.Fatal(err)
			}
			if key != want {
				t.Fatal("wrong edition request", key, want)
			}
			count++
		}
		rows.Close()
		if count != 1 {
			t.Fatal("wrong request count", count)
		}
	}
}
