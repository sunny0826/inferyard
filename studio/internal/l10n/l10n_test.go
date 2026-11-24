package l10n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"

	mygoi18n "github.com/egoist/mygo/i18n"
)

var placeholderPattern = regexp.MustCompile(`\{([a-zA-Z0-9_]+)(?:,|\})`)

func extractPlaceholders(s string) []string {
	matches := placeholderPattern.FindAllStringSubmatch(s, -1)
	seen := map[string]bool{}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) > 1 && !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

func containsCJKOrFullwidth(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || (r >= 0x3000 && r <= 0x303F) || (r >= 0xFF00 && r <= 0xFFEF) {
			return true
		}
	}
	return false
}

func TestCatalogKeyAndPlaceholderParity(t *testing.T) {
	en := mergedData[LanguageEn]
	zh := mergedData[LanguageZhHans]
	if len(en) == 0 || len(zh) == 0 {
		t.Fatalf("expected non-empty catalogs: en=%d zh-Hans=%d", len(en), len(zh))
	}
	for k, enVal := range en {
		zhVal, ok := zh[k]
		if !ok {
			t.Errorf("missing zh-Hans translation for key %q", k)
			continue
		}
		if strings.TrimSpace(enVal) == "" {
			t.Errorf("empty en translation for key %q", k)
		}
		if strings.TrimSpace(zhVal) == "" {
			t.Errorf("empty zh-Hans translation for key %q", k)
		}
		if _, err := mygoi18n.ParseMessage(enVal); err != nil {
			t.Errorf("invalid ICU message in en for %q: %v", k, err)
		}
		if _, err := mygoi18n.ParseMessage(zhVal); err != nil {
			t.Errorf("invalid ICU message in zh-Hans for %q: %v", k, err)
		}
		enPH := extractPlaceholders(enVal)
		zhPH := extractPlaceholders(zhVal)
		if !reflect.DeepEqual(enPH, zhPH) {
			t.Errorf("placeholder mismatch for key %q: en=%v zh-Hans=%v", k, enPH, zhPH)
		}
	}
	for k := range zh {
		if _, ok := en[k]; !ok {
			t.Errorf("extra zh-Hans key %q not present in en", k)
		}
	}
}

func TestNoCJKOrFullwidthInEnglishCatalog(t *testing.T) {
	for k, v := range mergedData[LanguageEn] {
		if k == "settings.language.zh_hans" && v == "简体中文" {
			continue
		}
		if containsCJKOrFullwidth(v) {
			t.Errorf("en catalog key %q contains CJK or fullwidth characters: %q", k, v)
		}
	}
}

func TestNoHardcodedCJKInStudioProdCode(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve current test file path")
	}
	studioRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	fset := token.NewFileSet()

	err := filepath.WalkDir(studioRoot, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			rel, _ := filepath.Rel(studioRoot, p)
			relSlash := filepath.ToSlash(rel)
			if relSlash == "tests" || relSlash == "internal/l10n" || relSlash == "build" || strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(studioRoot, p)
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			val, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			if containsCJKOrFullwidth(val) {
				pos := fset.Position(lit.Pos())
				t.Errorf("hardcoded CJK/fullwidth string in %s:%d: %q (move to studio/internal/l10n)", filepath.ToSlash(rel), pos.Line, val)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk studio sources: %v", err)
	}
}

func TestNormalizationAndLookup(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", LanguageSystem},
		{"system", LanguageSystem},
		{"auto", LanguageSystem},
		{"en", LanguageEn},
		{"en-US", LanguageEn},
		{"zh", LanguageZhHans},
		{"zh-CN", LanguageZhHans},
		{"zh-Hans", LanguageZhHans},
		{"zh_CN.UTF-8", LanguageZhHans},
		{"fr-FR", LanguageSystem},
	}
	for _, tc := range cases {
		if got := NormalizeLanguage(tc.in); got != tc.want {
			t.Errorf("NormalizeLanguage(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	if got := T(LanguageEn, "nav.settings"); got != "Settings" {
		t.Fatalf("en nav.settings = %q, want Settings", got)
	}
	if got := T(LanguageZhHans, "nav.settings"); got != "设置" {
		t.Fatalf("zh-Hans nav.settings = %q, want 设置", got)
	}
	if got := Tf(LanguageEn, "settings.language.current", "language", "English"); got != "Current: English" {
		t.Fatalf("en settings.language.current = %q", got)
	}
	if got := Tf(LanguageZhHans, "settings.language.current", "language", "简体中文"); got != "当前：简体中文" {
		t.Fatalf("zh-Hans settings.language.current = %q", got)
	}
}
