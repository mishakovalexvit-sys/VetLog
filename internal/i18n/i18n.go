package i18n

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed locales/*.yaml
var localesFS embed.FS

const DefaultLang = "ru"

var requiredLangs = []string{"ru", "en"}

// Bundle хранит плоские словари: ключ вида "form.consent.before" -> строка.
type Bundle struct {
	dicts map[string]map[string]string
}

func Load() (*Bundle, error) {
	return LoadFS(localesFS, "locales")
}

// LoadFS читает YAML-словари (вложенные или плоские ключи), превращает их в плоские
// ключи через точку и проверяет, что ключи в языках совпадают.
func LoadFS(fsys fs.FS, dir string) (*Bundle, error) {
	files, err := fs.Glob(fsys, path.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	b := &Bundle{dicts: make(map[string]map[string]string, len(files))}
	for _, f := range files {
		raw, err := fs.ReadFile(fsys, f)
		if err != nil {
			return nil, err
		}
		var tree map[string]any
		if err := yaml.Unmarshal(raw, &tree); err != nil {
			return nil, fmt.Errorf("i18n: %s: %w", f, err)
		}
		flat := make(map[string]string)
		if err := flatten("", tree, flat); err != nil {
			return nil, fmt.Errorf("i18n: %s: %w", f, err)
		}
		b.dicts[strings.TrimSuffix(path.Base(f), ".yaml")] = flat
	}
	if err := b.checkParity(); err != nil {
		return nil, err
	}
	return b, nil
}

// flatten: form: {name: x} -> "form.name": "x". Числовые ключи (1:, 2:) становятся "1", "2".
// Значения должны быть строками: списки и числа в словарях не поддерживаются.
func flatten(prefix string, v any, out map[string]string) error {
	switch t := v.(type) {
	case map[string]any:
		for k, sub := range t {
			if err := flatten(join(prefix, k), sub, out); err != nil {
				return err
			}
		}
	case map[any]any:
		for k, sub := range t {
			if err := flatten(join(prefix, fmt.Sprint(k)), sub, out); err != nil {
				return err
			}
		}
	case string:
		if prefix == "" {
			return fmt.Errorf("empty key")
		}
		out[prefix] = t
	default:
		return fmt.Errorf("key %q: value must be a string, got %T", prefix, v)
	}
	return nil
}

func join(prefix, k string) string {
	if prefix == "" {
		return k
	}
	return prefix + "." + k
}

func (b *Bundle) checkParity() error {
	for _, lang := range requiredLangs {
		if _, ok := b.dicts[lang]; !ok {
			return fmt.Errorf("i18n: dictionary %q not found", lang)
		}
	}
	all := make(map[string]struct{})
	for _, d := range b.dicts {
		for k := range d {
			all[k] = struct{}{}
		}
	}
	var missing []string
	for lang, d := range b.dicts {
		for k := range all {
			if _, ok := d[k]; !ok {
				missing = append(missing, lang+":"+k)
			}
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("i18n: keys missing in dictionaries: %s", strings.Join(missing, ", "))
	}
	return nil
}

// Has сообщает, есть ли ключ в словаре языка.
func (b *Bundle) Has(lang, key string) bool {
	_, ok := b.dicts[lang][key]
	return ok
}

// Func возвращает функцию перевода. Неизвестный ключ возвращается как есть (видно в выдаче,
// поэтому тест TestTemplateKeysExist ловит такие ключи до деплоя).
func (b *Bundle) Func(lang string) func(string) string {
	d, ok := b.dicts[lang]
	if !ok {
		d = b.dicts[DefaultLang]
	}
	return func(key string) string {
		if v, ok := d[key]; ok {
			return v
		}
		return key
	}
}
