package i18n

import "testing"

func TestCatalogParity(t *testing.T) {
	en, err := Load("en")
	if err != nil {
		t.Fatal(err)
	}
	zh, err := Load("zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	if len(en) != len(zh) {
		t.Fatal("catalog sizes differ")
	}
	for key, value := range en {
		if value == "" || zh[key] == "" {
			t.Errorf("missing translation: %s", key)
		}
	}
	if _, err = Load("fr"); err == nil {
		t.Fatal("unsupported language accepted")
	}
}
