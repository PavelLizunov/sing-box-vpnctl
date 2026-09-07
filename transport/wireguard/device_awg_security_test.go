//go:build with_awg

package wireguard

import (
 "reflect"
 "strings"
 "testing"

 "github.com/sagernet/sing-box/option"
)

func TestAwgRejectLineInjection(t *testing.T) {
 for _, field := range []string{"I1", "I2", "I3", "I4", "I5", "ContentPaddingAddition", "RekeyAfterTime", "HeaderProtectionKey"} {
  for _, newline := range []string{"\r", "\n", "\r\n"} {
   t.Run(field+newline, func(t *testing.T) {
    var options option.AmneziaWGOptions
    const secret = "DO_NOT_LEAK_SECRET"
    reflect.ValueOf(&options).Elem().FieldByName(field).SetString(secret+newline+"replace_peers=true")
    lines, err := awgIpcLines(options)
    if err == nil || lines != "" { t.Fatal("accepted line injection") }
    if strings.Contains(err.Error(), secret) { t.Fatal("error exposed input") }
   })
  }
 }
}
