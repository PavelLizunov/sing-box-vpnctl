package v2rayxhttp

import (
	"reflect"

	"github.com/sagernet/sing-box/common/tls"
)

const realityConfigTypeName = "RealityClientConfig"

const ktlsConfigTypeName = "KTLSClientConfig"

func tlsConfigIsReality(tlsConfig tls.Config) bool {
	return typeIsReality(tlsConfig, 0)
}

func typeIsReality(v any, depth int) bool {
	if v == nil || depth > 4 {
		return false
	}
	t := reflect.TypeOf(v)
	for t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t == nil {
		return false
	}
	name := t.Name()
	if name == realityConfigTypeName {
		return true
	}
	if name == ktlsConfigTypeName {
		if inner := embeddedConfig(v); inner != nil {
			return typeIsReality(inner, depth+1)
		}
	}
	return false
}

func embeddedConfig(v any) any {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil
	}
	f := rv.FieldByName("Config")
	if !f.IsValid() || !f.CanInterface() {
		return nil
	}
	return f.Interface()
}
