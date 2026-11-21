package i18n

import (
	"testing"
	"testing/fstest"

	"vetlog/internal/lead"
)

func TestEmbeddedDictionariesAreComplete(t *testing.T) {
	if _, err := Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

func TestParity(t *testing.T) {
	tests := []struct {
		name    string
		ru, en  string
		wantErr bool
	}{
		{"same keys", "a: x\nb: y\n", "a: X\nb: Y\n", false},
		{"missing in en", "a: x\nb: y\n", "a: X\n", true},
		{"extra in en", "a: x\n", "a: X\nb: Y\n", true},
		{"en dictionary absent", "a: x\n", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fsys := fstest.MapFS{"locales/ru.yaml": {Data: []byte(tt.ru)}}
			if tt.en != "" {
				fsys["locales/en.yaml"] = &fstest.MapFile{Data: []byte(tt.en)}
			}
			_, err := LoadFS(fsys, "locales")
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestErrorCodesHaveTranslations(t *testing.T) {
	b, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"ru", "en"} {
		for _, code := range lead.ErrorCodes {
			key := "err." + code
			if _, ok := b.dicts[lang][key]; !ok {
				t.Errorf("%s: missing %s", lang, key)
			}
		}
	}
}
