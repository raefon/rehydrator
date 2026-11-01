package webui

import (
 "net/http/httptest"
 "regexp"
 "testing"
)
func TestEmbeddedAssetReferences(t *testing.T) {
 handler:=Handler();w:=httptest.NewRecorder();handler.ServeHTTP(w,httptest.NewRequest("GET","/",nil))
 if w.Code!=200 {t.Fatal(w.Code)}
 refs:=regexp.MustCompile(`(?:src|href)="(/assets/[^"]+)"`).FindAllStringSubmatch(w.Body.String(),-1)
 if len(refs)<2 {t.Fatal("production assets missing")}
 for _,ref:=range refs {w=httptest.NewRecorder();handler.ServeHTTP(w,httptest.NewRequest("GET",ref[1],nil));if w.Code!=200 {t.Fatal(ref[1],w.Code)}}
}
