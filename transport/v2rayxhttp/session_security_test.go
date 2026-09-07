package v2rayxhttp

import (
 "go/ast"
 "go/parser"
 "go/token"
 "strings"
 "testing"
)

func TestSessionAlphabetUniqueEntropy(t *testing.T) {
 for _, table := range []string{strings.Repeat("a", 64), strings.Repeat("ab", 32)} {
  if _, _, err := resolveSessionID(table, "6"); err == nil { t.Fatal("duplicate alphabet inflated entropy") }
 }
 table, _, err := resolveSessionID("aabb", "31")
 if err != nil { t.Fatal(err) }
 if table != "ab" { t.Fatalf("alphabet must be deduplicated, got %q", table) }
 if _, _, err := resolveSessionID("ab", "30"); err == nil { t.Fatal("accepted insufficient binary space") }
}

// This regression checks the security source requirement, not statistical
// properties that cannot distinguish a CSPRNG from a pseudorandom generator.
func TestSessionRandomSource(t *testing.T) {
 file, err := parser.ParseFile(token.NewFileSet(), "client.go", nil, 0)
 if err != nil { t.Fatal(err) }
 hasCrypto := false
 for _, imp := range file.Imports { if imp.Path.Value == `"crypto/rand"` { hasCrypto = true } }
 if !hasCrypto { t.Fatal("session IDs must use crypto/rand") }
 for _, decl := range file.Decls {
  fn, ok := decl.(*ast.FuncDecl)
  if !ok || (fn.Name.Name != "newSessionID" && fn.Name.Name != "newUUIDSessionID") { continue }
  ast.Inspect(fn.Body, func(n ast.Node) bool {
   if id, ok := n.(*ast.Ident); ok && id.Name == "randIntn" { t.Error("session IDs use padding PRNG") }
   return true
  })
 }
}
