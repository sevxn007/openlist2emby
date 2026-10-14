package metadata

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

var queryYearPattern = regexp.MustCompile(`(?:^|[ ._\\()（）\[\]【】\-])((?:19|20)[0-9]{2})(?:$|[ ._\\()（）\[\]【】\-])`)

// ParseQuery separates an explicitly delimited release year from a title.
// Bare numeric titles and numbers embedded in titles are left unchanged.
func ParseQuery(query string) (string, *int) {
	query = strings.TrimSpace(query)
	match := queryYearPattern.FindStringSubmatchIndex(query)
	if match == nil {
		return query, nil
	}
	name := strings.TrimSpace(query[:match[0]] + " " + query[match[1]:])
	name = strings.Trim(name, " ._\\-()（）[]【】")
	if name == "" {
		return query, nil
	}
	year, _ := strconv.Atoi(query[match[2]:match[3]])
	return name, &year
}

// FilterYear keeps only results with a known, matching release/first-air year.
func FilterYear(results []domain.MediaSearchResult, year *int) []domain.MediaSearchResult {
	if year == nil {
		return results
	}
	filtered := make([]domain.MediaSearchResult, 0, len(results))
	for _, result := range results {
		date := result.ReleaseDate
		if result.MediaType == "tv" || date == "" {
			date = result.FirstAirDate
		}
		if len(date) < 4 {
			continue
		}
		value, err := strconv.Atoi(date[:4])
		if err == nil && value == *year {
			filtered = append(filtered, result)
		}
	}
	return filtered
}
