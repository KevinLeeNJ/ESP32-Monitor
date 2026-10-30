//go:build integration

package integration_test

import (
 "bytes"
 "context"
 "encoding/json"
 "fmt"
 "io"
 "log/slog"
 "net/http"
 "net/http/cookiejar"
 "net/http/httptest"
 "path/filepath"
 "regexp"
 "strings"
 "sync/atomic"
 "testing"
 "time"

 "esp32-monitor/server/internal/api"
 "esp32-monitor/server/internal/config"
 "esp32-monitor/server/internal/storage"
)
// Exercise real cookies and TCP plus reopening SQLite, not just a retained Handler.
func TestV2LoopbackAndPersistentCookie(t *testing.T){
 cfg:=config.Defaults();cfg.Database=filepath.Join(t.TempDir(),"monitor.db");cfg.ReadToken=strings.Repeat("1",64)
 repo,err:=storage.Open(context.Background(),cfg.Database,nil,time.Now().Unix());if err!=nil{t.Fatal(err)};defer func(){repo.Close()}()
 var output bytes.Buffer
 h,err:=api.New(cfg,repo,time.Now,slog.New(slog.NewJSONHandler(io.Discard,nil)),&output);if err!=nil{t.Fatal(err)}
 var current atomic.Pointer[api.Handler];current.Store(h);srv:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){current.Load().ServeHTTP(w,r)}));defer func(){srv.Close()}();jar,_:=cookiejar.New(nil);client:=&http.Client{Jar:jar,Timeout:10*time.Second}
 csrf:=""
 do:=func(method,path,token,body string,want int,headers ...string)[]byte{t.Helper();req,err:=http.NewRequest(method,srv.URL+path,strings.NewReader(body));if err!=nil{t.Fatal(err)};req.Header.Set("Content-Type","application/json");req.Header.Set("Origin",srv.URL);if token!=""{req.Header.Set("Authorization","Bearer "+token)};if csrf!=""{req.Header.Set("X-CSRF-Token",csrf)};for i:=0;i<len(headers);i+=2{req.Header.Set(headers[i],headers[i+1])};res,err:=client.Do(req);if err!=nil{t.Fatal(err)};defer res.Body.Close();raw,_:=io.ReadAll(res.Body);if res.StatusCode!=want{t.Fatalf("%s %s: %d %s",method,path,res.StatusCode,raw)};return raw}
 temp:=regexp.MustCompile(`WebUI 临时密码: ([a-f0-9]+)`).FindStringSubmatch(output.String())[1]
 var state struct{CSRF string `json:"csrf_token"`}
 _ =json.Unmarshal(do("POST","/api/auth/login","",fmt.Sprintf(`{"password":%q}`,temp),200),&state);csrf=state.CSRF
 _ =json.Unmarshal(do("POST","/api/auth/password","",fmt.Sprintf(`{"current_password":%q,"new_password":"loopback permanent password"}`,temp),200),&state);csrf=state.CSRF
 var device struct{ID,Code string};_ =json.Unmarshal(do("POST","/api/devices","",`{"name":"PC"}`,201),&device)
 var claim struct{Token string};_ =json.Unmarshal(do("POST","/v2/collector/register","",fmt.Sprintf(`{"code":%q}`,device.Code),200),&claim)
 profile:=`{"version":2,"interval_seconds":10,"enabled":[],"inventory":{"memory_total":"17179869184"}}`
 do("PUT","/v2/collector/profile",claim.Token,profile,200,"If-None-Match","*")
 body:=fmt.Sprintf(`{"r":1,"id":"0123456789abcdef0123456789abcdef","s":[{"t":%d,"c":1250,"m":6200,"u":86400,"mem":{"u":"10651518976"}}]}`,time.Now().Unix())
 do("POST","/v2/collector/samples",claim.Token,body,204)
 // Reopen the DB while keeping the same browser cookie and HTTP listener.
 repo.Close();repo,err=storage.Open(context.Background(),cfg.Database,nil,time.Now().Unix());if err!=nil{t.Fatal(err)};output.Reset();h,err=api.New(cfg,repo,time.Now,slog.Default(),&output);if err!=nil{t.Fatal(err)}
 // Replace handler only while no requests are active.
 current.Store(h)
 if output.Len()!=0{t.Fatal("regenerated completed password")}
 do("GET","/api/devices","","",200);do("POST","/v2/collector/samples",claim.Token,body,204)
 raw:=do("GET","/v2/devices/summary",cfg.ReadToken,"",200);if !bytes.Contains(raw,[]byte(`"device_count":1`)){t.Fatal(string(raw))}
 do("GET","/v1/devices/summary",cfg.ReadToken,"",404)
}
