package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed *.json
var files embed.FS

type Catalog map[string]string

func Load(locale string) (Catalog, error) {
	locale = strings.ToLower(strings.ReplaceAll(locale, "_", "-"))
	switch locale {
	case "", "en", "en-us", "en-gb":
		locale = "en"
	case "zh", "zh-cn", "zh-hans":
		locale = "zh-CN"
	default:
		return nil, fmt.Errorf("unsupported language %q; use en or zh-CN", locale)
	}
	data, err := files.ReadFile(locale + ".json")
	if err != nil {
		return nil, err
	}
	var c Catalog
	err = json.Unmarshal(data, &c)
	return c, err
}
func (c Catalog) Text(key string) string {
	if s, ok := c[key]; ok {
		return s
	}
	return key
}
