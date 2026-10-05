package option

import (
	"strconv"
	"strings"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/json"
)

type MagicHeader string

func (h *MagicHeader) UnmarshalJSON(data []byte) error {
	var spec string
	if len(data) > 0 && data[0] == '"' {
		err := json.Unmarshal(data, &spec)
		if err != nil {
			return err
		}
	} else {
		var value uint32
		err := json.Unmarshal(data, &value)
		if err != nil {
			return E.New("invalid magic header ", string(data), ": expected uint32 or \"min-max\" string")
		}
		spec = strconv.FormatUint(uint64(value), 10)
	}
	normalized, err := normalizeMagicHeader(spec)
	if err != nil {
		return err
	}
	*h = normalized
	return nil
}

func (h MagicHeader) MarshalJSON() ([]byte, error) {
	normalized, err := normalizeMagicHeader(string(h))
	if err != nil {
		return nil, err
	}
	if normalized == "" {
		return []byte("0"), nil
	}
	if !strings.Contains(string(normalized), "-") {
		return []byte(normalized), nil
	}
	return json.Marshal(string(normalized))
}

func (h MagicHeader) Spec() (string, error) {
	normalized, err := normalizeMagicHeader(string(h))
	return string(normalized), err
}

func normalizeMagicHeader(spec string) (MagicHeader, error) {
	if strings.ContainsAny(spec, "\r\n") {
		return "", E.New("magic header must not contain CR or LF")
	}
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", nil
	}
	part0, part1, hasDash := strings.Cut(spec, "-")
	if hasDash && strings.Contains(part1, "-") {
		return "", E.New("invalid magic header ", strconv.Quote(spec), ": expected uint32 or \"min-max\"")
	}
	start, err := strconv.ParseUint(strings.TrimSpace(part0), 10, 32)
	if err != nil {
		return "", E.New("invalid magic header ", strconv.Quote(spec), ": parse ", strconv.Quote(part0), ": ", err)
	}
	end := start
	if hasDash {
		end, err = strconv.ParseUint(strings.TrimSpace(part1), 10, 32)
		if err != nil {
			return "", E.New("invalid magic header ", strconv.Quote(spec), ": parse ", strconv.Quote(part1), ": ", err)
		}
	}
	if end < start {
		return "", E.New("invalid magic header ", strconv.Quote(spec), ": range start > end")
	}
	if end == 0 {
		return "", nil
	}
	if start == end {
		return MagicHeader(strconv.FormatUint(start, 10)), nil
	}
	return MagicHeader(strconv.FormatUint(start, 10) + "-" + strconv.FormatUint(end, 10)), nil
}

type AmneziaWGOptions struct {
	Jc   uint32      `json:"jc,omitempty"`
	Jmin uint32      `json:"jmin,omitempty"`
	Jmax uint32      `json:"jmax,omitempty"`
	S1   uint32      `json:"s1,omitempty"`
	S2   uint32      `json:"s2,omitempty"`
	S3   uint32      `json:"s3,omitempty"`
	S4   uint32      `json:"s4,omitempty"`
	H1   MagicHeader `json:"h1,omitempty"`
	H2   MagicHeader `json:"h2,omitempty"`
	H3   MagicHeader `json:"h3,omitempty"`
	H4   MagicHeader `json:"h4,omitempty"`
	I1   string      `json:"i1,omitempty"`
	I2   string      `json:"i2,omitempty"`
	I3   string      `json:"i3,omitempty"`
	I4   string      `json:"i4,omitempty"`
	I5   string      `json:"i5,omitempty"`
	Id   string      `json:"id,omitempty"`
	Ip   string      `json:"ip,omitempty"`
	Ib   string      `json:"ib,omitempty"`

	RandomTrailers         *bool  `json:"random_trailers,omitempty"`
	DisableCookie          *bool  `json:"disable_cookies,omitempty"`
	HeaderProtectionKey    string `json:"header_protection_key,omitempty"`
	ContentPaddingAddition string `json:"content_padding_addition,omitempty"`
	RekeyAfterTime         string `json:"rekey_after_time,omitempty"`
}

func (o AmneziaWGOptions) IsSet() bool {
	return o != AmneziaWGOptions{}
}
