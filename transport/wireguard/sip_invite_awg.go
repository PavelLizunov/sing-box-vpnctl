//go:build with_awg

package wireguard

import (
	"strings"
)

type sipDialog struct {
	calleeHost string
	callerDom  string
	uaHost     string
	fromUser   string
	toUser     string
	fromDisp   string
	toDisp     string
	branch     string
	tag        string
	callID     string
	cseq       string
}

func newSIPDialog(domain string) sipDialog {
	callerDom := strings.TrimSuffix(domain, ".")
	if callerDom == "" {
		callerDom = pgDomainHost()
	}
	fromUser := pgUser()
	toUser := pgUser()
	return sipDialog{
		calleeHost: pgDomainHost(),
		callerDom:  callerDom,
		uaHost:     "pc" + pgDigits(2) + "." + callerDom,
		fromUser:   fromUser,
		toUser:     toUser,
		fromDisp:   sipCapitalize(fromUser),
		toDisp:     sipCapitalize(toUser),
		branch:     pgHex(16),
		tag:        pgDigits(10),
		callID:     pgHex(14),
		cseq:       pgDigits(6),
	}
}

func masqueSIPInviteCPS(d sipDialog) (string, error) {
	var s strings.Builder
	s.WriteString("INVITE sip:" + d.toUser + "@" + d.calleeHost + " SIP/2.0\r\n")
	s.WriteString("Via: SIP/2.0/UDP " + d.uaHost + ";branch=z9hG4bK" + d.branch + "\r\n")
	s.WriteString("Max-Forwards: 70\r\n")
	s.WriteString("To: " + d.toDisp + " <sip:" + d.toUser + "@" + d.calleeHost + ">\r\n")
	s.WriteString("From: " + d.fromDisp + " <sip:" + d.fromUser + "@" + d.callerDom + ">;tag=" + d.tag + "\r\n")
	s.WriteString("Call-ID: " + d.callID + "@" + d.uaHost + "\r\n")
	s.WriteString("CSeq: " + d.cseq + " INVITE\r\n")
	s.WriteString("Contact: <sip:" + d.fromUser + "@" + d.uaHost + ">\r\n")
	s.WriteString("Content-Type: application/sdp\r\n")
	s.WriteString("Content-Length: 0\r\n")
	s.WriteString("\r\n")

	var b cpsBuilder
	b.addBytes([]byte(s.String()))
	return b.String(), nil
}

func masqueSIPTryingCPS(d sipDialog) (string, error) {
	var s strings.Builder
	s.WriteString("SIP/2.0 100 Trying\r\n")
	s.WriteString("Via: SIP/2.0/UDP " + d.uaHost + ";branch=z9hG4bK" + d.branch + "\r\n")
	s.WriteString("To: " + d.toDisp + " <sip:" + d.toUser + "@" + d.calleeHost + ">\r\n")
	s.WriteString("From: " + d.fromDisp + " <sip:" + d.fromUser + "@" + d.callerDom + ">;tag=" + d.tag + "\r\n")
	s.WriteString("Call-ID: " + d.callID + "@" + d.uaHost + "\r\n")
	s.WriteString("CSeq: " + d.cseq + " INVITE\r\n")
	s.WriteString("Content-Length: 0\r\n")
	s.WriteString("\r\n")

	var b cpsBuilder
	b.addBytes([]byte(s.String()))
	return b.String(), nil
}

func sipCapitalize(s string) string {
	if s == "" {
		return s
	}
	if c := s[0]; c >= 'a' && c <= 'z' {
		return string(c-('a'-'A')) + s[1:]
	}
	return s
}
