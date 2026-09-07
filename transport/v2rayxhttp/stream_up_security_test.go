package v2rayxhttp

import (
 "context"
 "io"
 "strings"
 "strconv"
 "sync/atomic"
 "sync"
 "testing"
 "time"

 M "github.com/sagernet/sing/common/metadata"
)

func TestStreamUpRejectUploadStatus(t *testing.T) {
 for _, status := range []int{204, 301, 403, 500} {
 t.Run(strconv.Itoa(status), func(t *testing.T) {
 hang := &hangRoundTripper{}
 client := stubClient(t, modeStreamUp, methodRoundTripper{get: hang, post: statusRoundTripper{code: status}})
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
 })
 }
}

type closeCountReader struct { closes atomic.Int32 }
func (r *closeCountReader) Read(p []byte) (int,error) { return 0, io.EOF }
func (r *closeCountReader) Close() error { r.closes.Add(1); return nil }

func TestSplitFailureClosesBoundAndLateBodies(t *testing.T) {
 for _, bound := range []bool{false, true} {
  pr,pw := io.Pipe()
  client := &xmuxClient{openUsage:1}
  conn := newSplitConn(pr,pw,M.Socksaddr{},newXmuxRelease(client))
  reader := &closeCountReader{}
  if bound { conn.setupReader(reader,nil) }
  conn.uploadFailed(io.ErrUnexpectedEOF)
  if !bound { conn.setupReader(reader,nil) }
  conn.fail(io.EOF)
  conn.Close()
  if got := reader.closes.Load(); got != 1 { t.Fatalf("body closed %d times",got) }
  if got := client.getOpenUsage(); got != 0 { t.Fatalf("XMUX usage %d",got) }
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

func TestSplitFailureStopsTimers(t *testing.T) {
 pr, pw := io.Pipe()
 conn := newSplitConn(pr, pw, M.Socksaddr{}, nil)
 defer conn.Close()
 conn.SetDeadline(time.Now().Add(time.Hour))
 conn.uploadFailed(io.ErrUnexpectedEOF)
 conn.SetDeadline(time.Now().Add(time.Hour))
 conn.readDeadline.access.Lock()
 readStopped := conn.readDeadline.timer == nil
 conn.readDeadline.access.Unlock()
 conn.writeDeadline.access.Lock()
 writeStopped := conn.writeDeadline.timer == nil
 conn.writeDeadline.access.Unlock()
 if !readStopped || !writeStopped { t.Fatal("terminal failure retained deadline timers") }
}
