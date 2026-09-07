package v2rayxhttp

import (
 "context"
 "io"
 "net/http"
 "strings"
 "sync"
 "testing"
 "time"

 M "github.com/sagernet/sing/common/metadata"
)

func TestStreamUpRejectUploadStatus(t *testing.T) {
 hang := &hangRoundTripper{}
 client := stubClient(t, modeStreamUp, methodRoundTripper{get: hang, post: statusRoundTripper{code: 403}})
 conn, err := client.DialContext(context.Background())
 if err != nil { t.Fatal(err) }
 defer conn.Close()
 select {
 case err := <-writeUnderTest(conn, []byte("hello")):
  if err == nil || !strings.Contains(err.Error(), "upload status") { t.Fatalf("lost upload rejection: %v", err) }
 case <-time.After(time.Second): t.Fatal("upload rejection left Write blocked")
 }
 readErr := make(chan error, 1)
 go func() { _, err := conn.Read(make([]byte, 1)); readErr <- err }()
 select {
 case err := <-readErr:
  if err == nil || !strings.Contains(err.Error(), "upload status") { t.Fatalf("lost download error: %v", err) }
 case <-time.After(time.Second): t.Fatal("upload rejection left Read blocked")
 }
}

func TestSplitConnTerminalRaces(t *testing.T) {
 for i := 0; i < 100; i++ {
  pr, pw := io.Pipe()
  conn := newSplitConn(pr, pw, M.Socksaddr{}, nil)
  reader, writer := io.Pipe()
  var wg sync.WaitGroup
  for _, f := range []func(){func(){conn.setupReader(reader,nil)}, func(){conn.uploadFailed(io.ErrUnexpectedEOF)}, func(){conn.Close()}, func(){conn.SetReadDeadline(time.Now())}} {
   wg.Add(1); go func(f func()){ defer wg.Done(); f() }(f)
  }
  wg.Wait()
  writer.Close()
  conn.Close()
 }
}

var _ = http.StatusOK
