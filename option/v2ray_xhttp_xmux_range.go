package option

import (
	"strconv"
	"strings"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/json"
)

type XmuxRange string

func (r XmuxRange) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(r))
}

func (r *XmuxRange) UnmarshalJSON(content []byte) error {
	var stringValue string
	if err := json.Unmarshal(content, &stringValue); err == nil {
		*r = XmuxRange(strings.TrimSpace(stringValue))
		return nil
	}
	var numberValue int
	if err := json.Unmarshal(content, &numberValue); err == nil {
		*r = XmuxRange(strconv.Itoa(numberValue))
		return nil
	}
	var arrayValue []int
	if err := json.Unmarshal(content, &arrayValue); err != nil {
		return E.New("invalid xmux range: expected \"min-max\", a number, or [min,max]")
	}
	switch len(arrayValue) {
	case 1:
		*r = XmuxRange(strconv.Itoa(arrayValue[0]))
	case 2:
		*r = XmuxRange(strconv.Itoa(arrayValue[0]) + "-" + strconv.Itoa(arrayValue[1]))
	default:
		return E.New("invalid xmux range: array form takes 1 or 2 elements, got ", len(arrayValue))
	}
	return nil
}
