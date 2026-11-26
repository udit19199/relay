package cache

import (
	"bytes"
	"fmt"

	"github.com/MoeForge/moego/base64"
	"github.com/MoeForge/moego/ojson"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
)

func marshalReasoningReplayRawJSONArray(items [][]byte) ([]byte, error) {
	doc := ojson.NewDocString("[]")
	for i, item := range items {
		trimmed := bytes.TrimSpace(item)
		if len(trimmed) == 0 || !ojson.ValidBytes(trimmed) {
			return nil, fmt.Errorf("invalid replay item json at index %d", i)
		}
		if errSet := doc.SetRawBytes("-1", trimmed); errSet != nil {
			return nil, errSet
		}
	}
	return doc.Bytes(), nil
}

func unmarshalReasoningReplayRawJSONArray(raw []byte) ([][]byte, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || !ojson.ValidBytes(trimmed) {
		return nil, fmt.Errorf("invalid reasoning replay KV payload (please upgrade CLIProxyAPI to a matching version)")
	}
	root := util.ParseGJSONBytesNoCopy(trimmed)
	if !root.IsArray() {
		return nil, fmt.Errorf("unsupported reasoning replay KV format (please upgrade CLIProxyAPI to a matching version)")
	}
	elems := root.Array()
	items := make([][]byte, 0, len(elems))
	for _, elem := range elems {
		switch {
		case elem.IsObject():
			items = append(items, []byte(elem.Raw))
		case elem.Type == ojson.String:
			decoded, errDecode := base64.StdEncoding.DecodeString(elem.String())
			if errDecode != nil {
				return nil, fmt.Errorf("invalid legacy base64 reasoning replay item (please upgrade CLIProxyAPI): %w", errDecode)
			}
			items = append(items, decoded)
		default:
			return nil, fmt.Errorf("unsupported reasoning replay KV element type (please upgrade CLIProxyAPI to a matching version)")
		}
	}
	return items, nil
}
