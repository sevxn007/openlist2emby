package music

import (
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestKuwoSearchMapsResultsAndRequest(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.Host != "www.kuwo.cn" || request.URL.Path != "/api/www/search/searchMusicBykeyWord" {
			t.Fatalf("request = %s %s", request.Method, request.URL)
		}
		query := request.URL.Query()
		if query.Get("key") != "晴天 周杰伦" || query.Get("pn") != "1" || query.Get("rn") != "10" || query.Get("httpsStatus") != "1" {
			t.Fatalf("query = %#v", query)
		}
		assertKuwoHeaders(t, request)
		return jsonResponse(`{"code":200,"data":{"list":[
			{"rid":228908,"name":" 晴天 ","artist":"周杰伦&温岚","album":"叶惠美","duration":"04:29","track":"3","disc":1},
			{"rid":"","name":"invalid"}
		]}}`), nil
	})})

	results, err := client.Search(t.Context(), "晴天 周杰伦", "kuwo", 10)
	if err != nil || len(results) != 1 {
		t.Fatalf("results = %#v, error = %v", results, err)
	}
	track := results[0]
	if track.ID != "228908" || track.Name != "晴天" || track.Album != "叶惠美" || track.Source != "kuwo" || track.PictureID != "228908" || track.LyricID != "228908" {
		t.Fatalf("track = %#v", track)
	}
	if len(track.Artists) != 2 || track.Artists[0] != "周杰伦" || track.Artists[1] != "温岚" || track.DurationMS != 269000 || track.TrackNumber != 3 || track.DiscNumber != 1 {
		t.Fatalf("metadata = %#v", track)
	}
}

func TestKuwoDetailLoadsCoverAndFormatsLyrics(t *testing.T) {
	jpeg := []byte{0xff, 0xd8, 0xff, 0xdb, 0x00, 0x43}
	client := NewClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api/www/music/musicInfo":
			if request.URL.Query().Get("mid") != "228908" {
				t.Fatalf("cover query = %#v", request.URL.Query())
			}
			assertKuwoHeaders(t, request)
			return jsonResponse(`{"code":200,"data":{"pic":"https://img.example/cover.jpg"}}`), nil
		case "/newh5/singles/songinfoandlrc":
			query := request.URL.Query()
			if query.Get("musicId") != "228908" || query.Get("mid") != "228908" || query.Get("plat") != "web_www" {
				t.Fatalf("lyrics query = %#v", query)
			}
			assertKuwoHeaders(t, request)
			return jsonResponse(`{"data":{"lrclist":[{"time":"1.25","lineLyric":"Original"},{"time":65.5,"lineLyric":"Second"},{"time":"70","lineLyric":""}]}}`), nil
		case "/cover.jpg":
			return imageResponse(jpeg), nil
		default:
			t.Fatalf("path = %q", request.URL.Path)
			return nil, nil
		}
	})})

	assets, err := client.Resolve(t.Context(), "kuwo", "228908", "228908")
	if err != nil {
		t.Fatal(err)
	}
	if assets.Detail.CoverURL != "https://img.example/cover.jpg" || assets.Detail.Lyrics != "[00:01.25]Original\n[01:05.50]Second" || assets.Detail.TranslatedLyrics != "" {
		t.Fatalf("detail = %#v", assets.Detail)
	}
	if assets.CoverMIME != "image/jpeg" || string(assets.CoverData) != string(jpeg) {
		t.Fatalf("cover = %q %x", assets.CoverMIME, assets.CoverData)
	}
}

func TestKuwoLyricsAllowsEmptyList(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/newh5/singles/songinfoandlrc" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		return jsonResponse(`{"data":{"lrclist":[]}}`), nil
	})})

	detail, err := client.Detail(t.Context(), "kuwo", "", "1")
	if err != nil || detail.Lyrics != "" || detail.TranslatedLyrics != "" {
		t.Fatalf("detail = %#v, error = %v", detail, err)
	}
}

func TestKuwoRejectsInvalidResponses(t *testing.T) {
	tests := []struct {
		name, operation, response, want string
	}{
		{name: "search error code", operation: "search", response: `{"code":401,"data":{"list":[]}}`, want: "music search failed"},
		{name: "malformed search", operation: "search", response: `{`, want: "invalid music search response"},
		{name: "invalid cover URL", operation: "cover", response: `{"code":200,"data":{"pic":"javascript:bad"}}`, want: "music service returned an invalid cover URL"},
		{name: "missing lyrics", operation: "lyrics", response: `{"data":null}`, want: "music lyrics request failed"},
		{name: "invalid lyrics", operation: "lyrics", response: `{"data":{"lrclist":[{"time":"bad","lineLyric":"text"}]}}`, want: "invalid music lyrics response"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := NewClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return jsonResponse(test.response), nil
			})})
			var err error
			switch test.operation {
			case "search":
				_, err = client.Search(t.Context(), "song", "kuwo", 20)
			case "cover":
				_, err = client.Detail(t.Context(), "kuwo", "1", "")
			case "lyrics":
				_, err = client.Detail(t.Context(), "kuwo", "", "1")
			}
			if err == nil || err.Error() != test.want {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestKuwoMapsUpstreamFailureAndResponseLimit(t *testing.T) {
	tests := []struct {
		name     string
		response *http.Response
	}{
		{
			name:     "upstream status",
			response: &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("unavailable"))},
		},
		{
			name:     "response too large",
			response: &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(io.LimitReader(zeroReader{}, maxJSONBodySize+1))},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := NewClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return test.response, nil
			})})
			_, err := client.Search(t.Context(), "song", "kuwo", 20)
			if err == nil {
				t.Fatal("expected an upstream error")
			}
			if status, ok := err.(interface{ StatusCode() int }); !ok || status.StatusCode() != http.StatusBadGateway {
				t.Fatalf("status error = %#v", err)
			}
		})
	}
}

func assertKuwoHeaders(t *testing.T, request *http.Request) {
	t.Helper()
	if request.Header.Get("User-Agent") == "" || request.Header.Get("Referer") != "https://www.kuwo.cn/" {
		t.Fatalf("headers = %#v", request.Header)
	}
	cookie, err := request.Cookie("Hm_token")
	if err != nil || len(cookie.Value) != kuwoTokenSize {
		t.Fatalf("cookie = %#v, error = %v", cookie, err)
	}
	shaSum := sha1.Sum([]byte(cookie.Value))
	crossSum := md5.Sum([]byte(hex.EncodeToString(shaSum[:]))) // #nosec G401 -- required by Kuwo's request protocol.
	if request.Header.Get("Cross") != hex.EncodeToString(crossSum[:]) {
		t.Fatalf("Cross = %q", request.Header.Get("Cross"))
	}
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = 0
	}
	return len(buffer), nil
}
