package sqlite

import (
	"strings"
	"unicode"

	"github.com/mozillazg/go-pinyin"
)

func mediaSortKey(value string) string {
	args := pinyin.NewArgs()
	var key strings.Builder
	for _, character := range strings.TrimSpace(value) {
		if unicode.Is(unicode.Han, character) {
			if syllables := pinyin.SinglePinyin(character, args); len(syllables) > 0 {
				key.WriteString(syllables[0])
				continue
			}
		}
		if unicode.IsLetter(character) || unicode.IsNumber(character) {
			key.WriteRune(unicode.ToLower(character))
			continue
		}
		key.WriteByte(' ')
	}
	return strings.Join(strings.Fields(key.String()), " ")
}
