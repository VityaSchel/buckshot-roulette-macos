package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strconv"
)

const maxSafeInteger = 1<<53 - 1

var errTrailingJSON = errors.New("unexpected data after JSON value")

// Numbers stay json.Number so a value out of float64 range only fails the field that holds it, as with JSON.parse and Zod.
func decodeJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(withoutBOM(data)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errTrailingJSON
	}
	return value, nil
}

func jsonObject(value any) (map[string]any, bool) {
	object, ok := value.(map[string]any)
	return object, ok
}

func finiteNumber(value any) (float64, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(string(number), 64)
	return parsed, err == nil
}

// Zod 4's int() accepts safe integers only.
func nonNegativeSafeInteger(value any) (int64, bool) {
	number, ok := finiteNumber(value)
	if !ok || number < 0 || number > maxSafeInteger || number != math.Trunc(number) {
		return 0, false
	}
	return int64(number), true
}
