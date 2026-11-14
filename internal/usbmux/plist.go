package usbmux

import (
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"sort"
	"strconv"
	"strings"
)

// XML property lists, as much of them as usbmuxd's messages use:
// dictionaries, arrays, strings, integers, reals, booleans and data.

func encodePlist(m map[string]any) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n" +
		`<plist version="1.0">`)
	writeValue(&b, m)
	b.WriteString("</plist>\n")
	return []byte(b.String())
}

func writeValue(b *strings.Builder, v any) {
	switch v := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString("<dict>")
		for _, k := range keys {
			b.WriteString("<key>" + html.EscapeString(k) + "</key>")
			writeValue(b, v[k])
		}
		b.WriteString("</dict>")
	case []any:
		b.WriteString("<array>")
		for _, e := range v {
			writeValue(b, e)
		}
		b.WriteString("</array>")
	case string:
		b.WriteString("<string>" + html.EscapeString(v) + "</string>")
	case int:
		b.WriteString("<integer>" + strconv.Itoa(v) + "</integer>")
	case int64:
		b.WriteString("<integer>" + strconv.FormatInt(v, 10) + "</integer>")
	case bool:
		if v {
			b.WriteString("<true/>")
		} else {
			b.WriteString("<false/>")
		}
	case []byte:
		b.WriteString("<data>" + base64.StdEncoding.EncodeToString(v) + "</data>")
	default:
		panic(fmt.Sprintf("usbmux: can't encode %T", v))
	}
}

// decodePlist reads a property list's one value: map[string]any, []any,
// string, int64, float64, bool or []byte.
func decodePlist(r io.Reader) (any, error) {
	d := xml.NewDecoder(r)
	for {
		tok, err := d.Token()
		if err != nil {
			return nil, err
		}
		if se, ok := tok.(xml.StartElement); ok {
			if se.Name.Local == "plist" {
				continue
			}
			return decodeValue(d, se)
		}
	}
}

func decodeValue(d *xml.Decoder, se xml.StartElement) (any, error) {
	switch se.Name.Local {
	case "dict":
		m := map[string]any{}
		key := ""
		for {
			tok, err := d.Token()
			if err != nil {
				return nil, err
			}
			switch t := tok.(type) {
			case xml.StartElement:
				if t.Name.Local == "key" {
					s, err := text(d)
					if err != nil {
						return nil, err
					}
					key = s
					continue
				}
				v, err := decodeValue(d, t)
				if err != nil {
					return nil, err
				}
				m[key] = v
			case xml.EndElement:
				return m, nil
			}
		}
	case "array":
		var a []any
		for {
			tok, err := d.Token()
			if err != nil {
				return nil, err
			}
			switch t := tok.(type) {
			case xml.StartElement:
				v, err := decodeValue(d, t)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			case xml.EndElement:
				return a, nil
			}
		}
	case "true", "false":
		if err := d.Skip(); err != nil {
			return nil, err
		}
		return se.Name.Local == "true", nil
	}
	s, err := text(d)
	if err != nil {
		return nil, err
	}
	switch se.Name.Local {
	case "string", "date":
		return s, nil
	case "integer":
		return strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	case "real":
		return strconv.ParseFloat(strings.TrimSpace(s), 64)
	case "data":
		return base64.StdEncoding.DecodeString(strings.Join(strings.Fields(s), ""))
	}
	return nil, errors.New("plist: unknown element " + se.Name.Local)
}

// text reads an element's text, through its end.
func text(d *xml.Decoder) (string, error) {
	var b strings.Builder
	for {
		tok, err := d.Token()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.CharData:
			b.Write(t)
		case xml.EndElement:
			return b.String(), nil
		}
	}
}
