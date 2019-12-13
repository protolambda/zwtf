package decode

import (
	"encoding/hex"
	"errors"
	"fmt"
	. "github.com/mitchellh/mapstructure"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

func decodeHex(src []byte, dst []byte) error {
	offset, byteCount := decodeHexOffsetAndLen(src)
	if len(dst) != byteCount {
		return errors.New("cannot decode hex, incorrect length")
	}
	_, err := hex.Decode(dst, src[offset:])
	return err
}

func decodeHexOffsetAndLen(src []byte) (offset int, length int) {
	if src[0] == '0' && src[1] == 'x' {
		offset = 2
	}
	return offset, hex.DecodedLen(len(src) - offset)
}

func decodeValue(v interface{}) interface{} {
	switch tv := v.(type) {
	case map[string]interface{}:
		return decodeStrKeyMap(tv)
	case map[interface{}]interface{}:
		return decodeMap(tv)
	case []interface{}:
		return decodeList(tv)
	default:
		return v
	}
}

var matchSnake = regexp.MustCompile("_([a-z0-9])")

func toPascalCase(str string) string {
	snake := []byte(matchSnake.ReplaceAllStringFunc(str, func(v string) string {
		return strings.ToUpper(string(v[1]))
	}))
	snake[0] = strings.ToUpper(string(snake[0]))[0]
	return string(snake)
}

func decodeList(v []interface{}) []interface{} {
	if len(v) == 0 {
		return nil
	}
	items := len(v)
	out := make([]interface{}, 0, items)
	for i := 0; i < items; i++ {
		out = append(out, decodeValue(v[i]))
	}
	return out
}

func decodeStrKeyMap(v map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{})
	for k, v := range v {
		out[toPascalCase(k)] = decodeValue(v)
	}
	return out
}

func decodeMap(v map[interface{}]interface{}) map[string]interface{} {
	out := make(map[string]interface{})
	for k, v := range v {
		kStr, ok := k.(string)
		if !ok {
			panic("cannot encode maps with non-string keys")
		}
		name := toPascalCase(kStr)
		out[name] = decodeValue(v)
	}
	return out
}

func decodeHook(s reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
	fmt.Println(data)
	if t.Kind() == reflect.Slice && t.Elem().Kind() != reflect.Uint8 {
		return data, nil
	}
	if s.Kind() != reflect.String {
		return data, nil
	}
	strData := data.(string)
	if t.Kind() == reflect.Array && t.Elem().Kind() == reflect.Uint8 {
		res := reflect.New(t).Elem()
		sliceRes := res.Slice(0, t.Len()).Interface()
		err := decodeHex([]byte(strData), sliceRes.([]byte))
		return res.Interface(), err
	}
	if t.Kind() == reflect.Uint64 {
		return strconv.ParseUint(strData, 10, 64)
	}
	if t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
		inBytes := []byte(strData)
		_, byteCount := decodeHexOffsetAndLen(inBytes)
		res := make([]byte, byteCount, byteCount)
		err := decodeHex([]byte(strData), res)
		return res, err
	}
	return data, nil
}

func DecodeJsonApiData(dst interface{}, rawData interface{}) error {
	var md Metadata
	config := &DecoderConfig{
		DecodeHook:       decodeHook,
		Metadata:         &md,
		WeaklyTypedInput: false,
		Result:           dst,
	}

	decoder, err := NewDecoder(config)
	if err != nil {
		return err
	}

	if err := decoder.Decode(decodeValue(rawData)); err != nil {
		return fmt.Errorf("cannot decode test-case: %v", err)
	}
	if len(md.Unused) > 0 {
		return errors.New(fmt.Sprintf("unused keys: %s", strings.Join(md.Unused, ", ")))
	}
	return nil
}
