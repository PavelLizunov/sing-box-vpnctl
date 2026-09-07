package device

import (
 "fmt"
 "strings"
 "testing"
)

func TestCPSPacketBounds(t *testing.T) {
 for _, tag := range []string{"r", "rc", "rd", "dz"} {
  for _, size := range []string{"-1", "65508", "1073741824", "9223372036854775808"} {
   spec := fmt.Sprintf("<%s %s>", tag, size)
   t.Run(spec, func(t *testing.T) {
    if _, err := newObfChain(spec); err == nil { t.Fatal("accepted invalid CPS size") }
   })
  }
  for _, size := range []int{0, 1, awgMaxPacketSize} {
   if _, err := newObfChain(fmt.Sprintf("<%s %d>", tag, size)); err != nil { t.Fatal(err) }
  }
 }
 for _, spec := range []string{"<r 32754><rc 32754>", "<r 65507><t>", "<b " + strings.Repeat("aa", awgMaxPacketSize+1) + ">"} {
  if _, err := newObfChain(spec); err == nil { t.Fatal("accepted oversized chain") }
 }
}
