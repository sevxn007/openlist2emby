package tmdbclient

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	tmdb "github.com/cyruzin/golang-tmdb"
	"github.com/sevxn007/openlist2emby/backend/internal/domain"
)

const (
	defaultLanguage = "en-US"
	imageBaseURL    = "https://image.tmdb.org/t/p/w500"

	chineseNameWorkers = 4
)

type Error struct {
	status  int
	message string
	err     error
}

func (e *Error) Error() string {
	return e.message
}

func (e *Error) Unwrap() error {
	return e.err
}

func (e *Error) StatusCode() int {
	return e.status
}

func (e *Error) PublicMessage() string {
	return e.message
}

type Service struct {
	imageAPIBaseURL    string
	timeout            time.Duration
	personNamesMu      sync.Mutex
	chinesePersonNames map[int64]string
}

func NewService() *Service {
	return &Service{timeout: 15 * time.Second, chinesePersonNames: make(map[int64]string)}
}

func (s *Service) Test(_ context.Context, apiKey string) error {
	client, err := s.client(apiKey)
	if err != nil {
		return err
	}
	if _, err := client.GetConfigurationAPI(); err != nil {
		return classifyError(err)
	}
	return nil
}

func (s *Service) Search(_ context.Context, apiKey, query, language string) ([]domain.MediaSearchResult, error) {
	client, err := s.client(apiKey)
	if err != nil {
		return nil, err
	}
	result, err := client.GetSearchMulti(strings.TrimSpace(query), map[string]string{
		"language": normalizeLanguage(language),
	})
	if err != nil {
		return nil, classifyError(err)
	}
	return mapSearchResults(result), nil
}

func mapSearchResults(result *tmdb.SearchMulti) []domain.MediaSearchResult {
	if result == nil || result.SearchMultiResults == nil {
		return []domain.MediaSearchResult{}
	}
	items := make([]domain.MediaSearchResult, 0, len(result.Results))
	for _, item := range result.Results {
		if item.MediaType != "movie" && item.MediaType != "tv" {
			continue
		}
		displayTitle := item.Title
		if item.MediaType == "tv" {
			displayTitle = item.Name
		}
		items = append(items, domain.MediaSearchResult{
			ID:            item.ID,
			MediaType:     item.MediaType,
			Title:         displayTitle,
			Name:          item.Name,
			OriginalTitle: firstNonEmpty(item.OriginalTitle, item.OriginalName),
			Overview:      item.Overview,
			PosterPath:    item.PosterPath,
			PosterURL:     imageURL(item.PosterPath),
			ReleaseDate:   item.ReleaseDate,
			FirstAirDate:  item.FirstAirDate,
			VoteAverage:   item.VoteAverage,
			GenreIDs:      item.GenreIDs,
		})
	}
	return items
}

func (s *Service) Detail(_ context.Context, apiKey, mediaType string, id int64, season *int, language string) (domain.MediaDetail, error) {
	client, err := s.client(apiKey)
	if err != nil {
		return domain.MediaDetail{}, err
	}

	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	options := map[string]string{"language": normalizeLanguage(language)}
	switch mediaType {
	case "movie":
		movie, err := client.GetMovieDetails(int(id), options)
		if err != nil {
			return domain.MediaDetail{}, classifyError(err)
		}
		return domain.MediaDetail{
			ID:            movie.ID,
			MediaType:     "movie",
			Title:         movie.Title,
			OriginalTitle: movie.OriginalTitle,
			Overview:      movie.Overview,
			PosterPath:    movie.PosterPath,
			PosterURL:     imageURL(movie.PosterPath),
			BackdropPath:  movie.BackdropPath,
			BackdropURL:   imageURL(movie.BackdropPath),
			IMDbID:        movie.IMDbID,
			Genres:        genreNames(movie.Genres),
			Runtime:       movie.Runtime,
			ReleaseDate:   movie.ReleaseDate,
			VoteAverage:   movie.VoteAverage,
		}, nil
	case "tv":
		if season == nil {
			show, err := client.GetTVDetails(int(id), options)
			if err != nil {
				return domain.MediaDetail{}, classifyError(err)
			}
			seasons := make([]domain.MediaSeason, 0, len(show.Seasons))
			for _, item := range show.Seasons {
				seasons = append(seasons, mapSeason(item))
			}
			return domain.MediaDetail{
				ID:               show.ID,
				MediaType:        "tv",
				Title:            show.Name,
				Name:             show.Name,
				OriginalTitle:    show.OriginalName,
				Overview:         show.Overview,
				PosterPath:       show.PosterPath,
				PosterURL:        imageURL(show.PosterPath),
				BackdropPath:     show.BackdropPath,
				BackdropURL:      imageURL(show.BackdropPath),
				Genres:           genreNames(show.Genres),
				FirstAirDate:     show.FirstAirDate,
				VoteAverage:      show.VoteAverage,
				NumberOfSeasons:  show.NumberOfSeasons,
				NumberOfEpisodes: show.NumberOfEpisodes,
				Seasons:          seasons,
			}, nil
		}
		seasonDetail, err := client.GetTVSeasonDetails(int(id), *season, options)
		if err != nil {
			return domain.MediaDetail{}, classifyError(err)
		}
		return mapTVSeasonDetail(seasonDetail), nil
	default:
		return domain.MediaDetail{}, &Error{status: http.StatusBadRequest, message: "media type must be movie or tv"}
	}
}

// Credits returns cast and crew for one locally indexed media item. The
// caller resolves an episode to its parent TV series before invoking it.
func (s *Service) Credits(ctx context.Context, apiKey string, query domain.MediaCreditsQuery, language string) ([]domain.MediaPerson, error) {
	client, err := s.client(apiKey)
	if err != nil {
		return nil, err
	}
	if query.TMDBID <= 0 {
		return []domain.MediaPerson{}, nil
	}
	collector := newPersonCollector()
	options := map[string]string{"language": normalizeLanguage(language)}
	switch query.MediaType {
	case "movie":
		credits, creditErr := client.GetMovieCredits(int(query.TMDBID), options)
		if creditErr != nil {
			return nil, classifyError(creditErr)
		}
		for _, person := range credits.Cast {
			collector.add(person.ID, person.Name, person.ProfilePath, "Actor", person.Character, person.Order)
		}
		for index, person := range credits.Crew {
			collector.add(person.ID, person.Name, person.ProfilePath, crewType(person.Job), person.Job, 10000+index)
		}
	case "tv":
		if query.SeasonNumber == nil {
			credits, creditErr := client.GetTVCredits(int(query.TMDBID), options)
			if creditErr != nil {
				return nil, classifyError(creditErr)
			}
			for _, person := range credits.Cast {
				collector.add(person.ID, person.Name, person.ProfilePath, "Actor", person.Character, person.Order)
			}
			for index, person := range credits.Crew {
				collector.add(person.ID, person.Name, person.ProfilePath, crewType(person.Job), person.Job, 10000+index)
			}
			break
		}
		if query.EpisodeNumber == nil {
			credits, creditErr := client.GetTVSeasonCredits(int(query.TMDBID), *query.SeasonNumber, options)
			if creditErr != nil {
				return nil, classifyError(creditErr)
			}
			for _, person := range credits.Cast {
				collector.add(person.ID, person.Name, person.ProfilePath, "Actor", person.Character, person.Order)
			}
			for index, person := range credits.Crew {
				collector.add(person.ID, person.Name, person.ProfilePath, crewType(person.Job), person.Job, 10000+index)
			}
			break
		}
		episode, creditErr := client.GetTVEpisodeDetails(int(query.TMDBID), *query.SeasonNumber, *query.EpisodeNumber, map[string]string{
			"append_to_response": "credits",
			"language":           normalizeLanguage(language),
		})
		if creditErr != nil {
			return nil, classifyError(creditErr)
		}
		if episode.Credits == nil {
			return []domain.MediaPerson{}, nil
		}
		credits := episode.Credits
		for _, person := range credits.Cast {
			collector.add(person.ID, person.Name, person.ProfilePath, "Actor", person.Character, person.Order)
		}
		for _, person := range credits.GuestStars {
			collector.add(person.ID, person.Name, person.ProfilePath, "Actor", person.Character, person.Order)
		}
		for index, person := range credits.Crew {
			collector.add(person.ID, person.Name, person.ProfilePath, crewType(person.Job), person.Job, 10000+index)
		}
	default:
		return []domain.MediaPerson{}, nil
	}
	return s.localizePeople(ctx, client, collector.items(), language), nil
}

// SeasonEpisodeCredits returns guest cast and crew grouped by episode number.
// TMDB includes these credits in the season detail response, so the whole
// season can be resolved without issuing one request per episode.
func (s *Service) SeasonEpisodeCredits(ctx context.Context, apiKey string, query domain.MediaCreditsQuery, language string) (map[int][]domain.MediaPerson, error) {
	client, err := s.client(apiKey)
	if err != nil {
		return nil, err
	}
	if strings.ToLower(strings.TrimSpace(query.MediaType)) != "tv" || query.TMDBID <= 0 || query.SeasonNumber == nil {
		return map[int][]domain.MediaPerson{}, nil
	}

	season, err := client.GetTVSeasonDetails(int(query.TMDBID), *query.SeasonNumber, map[string]string{
		"language": normalizeLanguage(language),
	})
	if err != nil {
		return nil, classifyError(err)
	}
	episodes := mapTVSeasonEpisodeCredits(season)
	localized := s.localizePeople(ctx, client, collectSeasonEpisodePeople(episodes), language)
	applySeasonEpisodeLocalization(episodes, localized, language)
	return episodes, nil
}

func (s *Service) PersonProfile(_ context.Context, apiKey string, personID int64, language string) (string, error) {
	client, err := s.client(apiKey)
	if err != nil {
		return "", err
	}
	if personID <= 0 {
		return "", nil
	}
	details, err := client.GetPersonDetails(int(personID), map[string]string{"language": normalizeLanguage(language)})
	if err != nil {
		return "", classifyError(err)
	}
	if details == nil {
		return "", nil
	}
	return imageURL(details.ProfilePath), nil
}

type personCollector struct {
	byID map[int64]domain.MediaPerson
}

func newPersonCollector() *personCollector {
	return &personCollector{byID: map[int64]domain.MediaPerson{}}
}

func (c *personCollector) add(id int64, name, profilePath, personType, role string, order int) {
	if id <= 0 || strings.TrimSpace(name) == "" {
		return
	}
	personType = strings.TrimSpace(personType)
	if personType == "" {
		personType = "Crew"
	}
	role = strings.TrimSpace(role)
	if existing, found := c.byID[id]; found {
		if existing.ProfileURL == "" {
			existing.ProfileURL = imageURL(profilePath)
		}
		if existing.Type != "Actor" && personType == "Actor" {
			existing.Type = personType
		}
		if role != "" && !strings.Contains(existing.Role, role) {
			if existing.Role == "" {
				existing.Role = role
			} else {
				existing.Role += " · " + role
			}
		}
		if order < existing.SortOrder {
			existing.SortOrder = order
		}
		c.byID[id] = existing
		return
	}
	c.byID[id] = domain.MediaPerson{
		ID: id, Name: strings.TrimSpace(name), ProfileURL: imageURL(profilePath), Type: personType, Role: role, SortOrder: order,
	}
}

func (c *personCollector) items() []domain.MediaPerson {
	result := make([]domain.MediaPerson, 0, len(c.byID))
	for _, person := range c.byID {
		result = append(result, person)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].SortOrder == result[right].SortOrder {
			return result[left].Name < result[right].Name
		}
		return result[left].SortOrder < result[right].SortOrder
	})
	return result
}

func mapTVSeasonEpisodeCredits(season *tmdb.TVSeasonDetails) map[int][]domain.MediaPerson {
	result := make(map[int][]domain.MediaPerson)
	if season == nil {
		return result
	}
	for _, episode := range season.Episodes {
		collector := newPersonCollector()
		for _, person := range episode.GuestStars {
			collector.add(person.ID, person.Name, person.ProfilePath, "Actor", person.Character, person.Order)
		}
		for index, person := range episode.Crew {
			collector.add(person.ID, person.Name, person.ProfilePath, crewType(person.Job), person.Job, 10000+index)
		}
		result[episode.EpisodeNumber] = collector.items()
	}
	return result
}

func collectSeasonEpisodePeople(episodes map[int][]domain.MediaPerson) []domain.MediaPerson {
	episodeNumbers := make([]int, 0, len(episodes))
	for episodeNumber := range episodes {
		episodeNumbers = append(episodeNumbers, episodeNumber)
	}
	sort.Ints(episodeNumbers)

	collector := newPersonCollector()
	for _, episodeNumber := range episodeNumbers {
		for _, person := range episodes[episodeNumber] {
			collector.add(person.ID, person.Name, "", person.Type, person.Role, person.SortOrder)
		}
	}
	return collector.items()
}

func applySeasonEpisodeLocalization(episodes map[int][]domain.MediaPerson, localized []domain.MediaPerson, language string) {
	byID := make(map[int64]domain.MediaPerson, len(localized))
	for _, person := range localized {
		byID[person.ID] = person
	}
	chinese := normalizeLanguage(language) == "zh-CN"
	for episodeNumber, people := range episodes {
		for index := range people {
			if person, found := byID[people[index].ID]; found {
				people[index].Name = person.Name
				if people[index].ProfileURL == "" {
					people[index].ProfileURL = person.ProfileURL
				}
			}
			if chinese {
				people[index].DisplayRole = localizedCreditDisplayRole(people[index].Role, people[index].Type, language)
			}
		}
		episodes[episodeNumber] = people
	}
}

func crewType(job string) string {
	if value := strings.TrimSpace(job); value != "" {
		return value
	}
	return "Crew"
}

// localizePeople enriches a Chinese UI request only. Credits stay on demand;
// no person data is retained or fetched while the media library is scanned.
func (s *Service) localizePeople(ctx context.Context, client *tmdb.Client, people []domain.MediaPerson, language string) []domain.MediaPerson {
	if normalizeLanguage(language) != "zh-CN" || len(people) == 0 {
		return people
	}

	localized := append([]domain.MediaPerson(nil), people...)
	lookupIndexes := make([]int, 0, len(localized))
	for index := range localized {
		localized[index].DisplayRole = localizedCreditDisplayRole(localized[index].Role, localized[index].Type, language)
		if containsChineseText(localized[index].Name) {
			continue
		}
		if name, found := s.cachedChinesePersonName(localized[index].ID); found {
			if name != "" {
				localized[index].Name = name
			}
			continue
		}
		lookupIndexes = append(lookupIndexes, index)
	}
	if len(lookupIndexes) == 0 {
		return localized
	}

	jobs := make(chan int)
	var workers sync.WaitGroup
	for worker := 0; worker < min(chineseNameWorkers, len(lookupIndexes)); worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				if ctx.Err() != nil {
					continue
				}
				personID := localized[index].ID
				details, err := client.GetPersonDetails(int(personID), map[string]string{"language": "zh-CN"})
				if err == nil && details != nil {
					name := chinesePersonName(append([]string{details.Name}, details.AlsoKnownAs...)...)
					s.storeChinesePersonName(personID, name)
					if name != "" {
						localized[index].Name = name
					}
				}
			}
		}()
	}
	for _, index := range lookupIndexes {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	return localized
}

func (s *Service) cachedChinesePersonName(personID int64) (string, bool) {
	s.personNamesMu.Lock()
	defer s.personNamesMu.Unlock()
	name, found := s.chinesePersonNames[personID]
	return name, found
}

func (s *Service) storeChinesePersonName(personID int64, name string) {
	s.personNamesMu.Lock()
	defer s.personNamesMu.Unlock()
	if s.chinesePersonNames == nil {
		s.chinesePersonNames = make(map[int64]string)
	}
	s.chinesePersonNames[personID] = name
}

func localizedCreditDisplayRole(role, personType, language string) string {
	role = strings.TrimSpace(role)
	if normalizeLanguage(language) != "zh-CN" {
		return role
	}
	if containsChineseText(role) {
		return role
	}
	if label, found := chineseCreditTypes[strings.TrimSpace(personType)]; found {
		return label
	}
	return "工作人员"
}

var chineseCreditTypes = map[string]string{
	"Actor":                   "演员",
	"Assistant Director":      "副导演",
	"Casting":                 "选角",
	"Cinematography":          "摄影",
	"Composer":                "作曲",
	"Creator":                 "创作者",
	"Director":                "导演",
	"Editor":                  "剪辑",
	"Executive Producer":      "执行制片人",
	"Makeup Artist":           "化妆",
	"Original Music Composer": "原创音乐",
	"Producer":                "制片人",
	"Production Design":       "艺术指导",
	"Screenplay":              "编剧",
	"Sound":                   "声音",
	"Visual Effects":          "视觉效果",
	"Writer":                  "编剧",
}

func chinesePersonName(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if containsChineseText(value) {
			return value
		}
	}
	return ""
}

func containsChineseText(value string) bool {
	for _, char := range value {
		if unicode.Is(unicode.Han, char) {
			return true
		}
	}
	return false
}

func mapTVSeasonDetail(season *tmdb.TVSeasonDetails) domain.MediaDetail {
	seasonNumber := season.SeasonNumber
	episodes := make([]domain.MediaEpisode, 0, len(season.Episodes))
	for _, item := range season.Episodes {
		episodes = append(episodes, domain.MediaEpisode{
			ID:            item.ID,
			AirDate:       item.AirDate,
			EpisodeNumber: item.EpisodeNumber,
			Name:          item.Name,
			Overview:      item.Overview,
			Runtime:       item.Runtime,
			SeasonNumber:  item.SeasonNumber,
			StillPath:     item.StillPath,
			StillURL:      imageURL(item.StillPath),
			VoteAverage:   item.VoteAverage,
			VoteCount:     item.VoteCount,
		})
	}
	return domain.MediaDetail{
		ID:           season.ID,
		MediaType:    "tv",
		Name:         season.Name,
		Overview:     season.Overview,
		PosterPath:   season.PosterPath,
		PosterURL:    imageURL(season.PosterPath),
		FirstAirDate: season.AirDate,
		VoteAverage:  season.VoteAverage,
		SeasonNumber: &seasonNumber,
		Episodes:     episodes,
	}
}

func genreNames(genres []tmdb.Genre) []string {
	result := make([]string, 0, len(genres))
	for _, genre := range genres {
		if name := strings.TrimSpace(genre.Name); name != "" {
			result = append(result, name)
		}
	}
	return result
}

func (s *Service) client(apiKey string) (*tmdb.Client, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, domain.ErrTMDBKeyNotConfigured
	}
	client, err := tmdb.Init(apiKey)
	if err != nil {
		return nil, &Error{status: http.StatusBadRequest, message: "invalid tmdb api key", err: err}
	}
	client.SetClientConfig(http.Client{Timeout: s.timeout})
	return client, nil
}

func classifyError(err error) error {
	if err == nil {
		return nil
	}
	var apiError tmdb.Error
	if errors.As(err, &apiError) {
		switch apiError.StatusCode {
		case 7:
			return &Error{status: http.StatusUnauthorized, message: "tmdb api key was rejected", err: err}
		case 25:
			return &Error{status: http.StatusTooManyRequests, message: "tmdb rate limit reached", err: err}
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{status: http.StatusGatewayTimeout, message: "tmdb request timed out", err: err}
	}
	return &Error{status: http.StatusBadGateway, message: "tmdb request failed", err: err}
}

func mapSeason(item tmdb.Season) domain.MediaSeason {
	return domain.MediaSeason{
		ID:           item.ID,
		AirDate:      item.AirDate,
		EpisodeCount: item.EpisodeCount,
		Name:         item.Name,
		Overview:     item.Overview,
		PosterPath:   item.PosterPath,
		PosterURL:    imageURL(item.PosterPath),
		SeasonNumber: item.SeasonNumber,
		VoteAverage:  item.VoteAverage,
	}
}

func imageURL(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return strings.TrimRight(imageBaseURL, "/") + "/" + strings.TrimLeft(path, "/")
}

func normalizeLanguage(language string) string {
	language = strings.ToLower(strings.TrimSpace(language))
	if language == "zh" || strings.HasPrefix(language, "zh-") {
		return "zh-CN"
	}
	return defaultLanguage
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
