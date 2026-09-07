package option

import "testing"

func TestMagicHeaderRejectCRLF(t *testing.T) {
	for _, value := range []string{"1\n", "\r1", "1\r-2", "\n"} {
		if _, err := MagicHeader(value).Spec(); err == nil {
			t.Errorf("accepted CRLF in %q", value)
		}
	}
	for _, value := range []string{`"1\n"`, `"\r1"`} {
		var h MagicHeader
		if err := h.UnmarshalJSON([]byte(value)); err == nil {
			t.Errorf("accepted CRLF JSON %s", value)
		}
	}
}
